package extensions

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxArchiveBytes   = 256 << 20
	maxUnpackedBytes  = 1 << 30
	maxArchiveEntries = 100000
)

func zipPayload(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("extension archive is truncated")
	}
	if string(data[:4]) != "Cr24" {
		return data, nil
	}
	if len(data) < 12 {
		return nil, errors.New("CRX header is truncated")
	}
	var offset uint64
	switch binary.LittleEndian.Uint32(data[4:8]) {
	case 2:
		if len(data) < 16 {
			return nil, errors.New("CRX2 header is truncated")
		}
		offset = 16 + uint64(binary.LittleEndian.Uint32(data[8:12])) + uint64(binary.LittleEndian.Uint32(data[12:16]))
	case 3:
		offset = 12 + uint64(binary.LittleEndian.Uint32(data[8:12]))
	default:
		return nil, errors.New("unsupported CRX version")
	}
	if offset+4 > uint64(len(data)) {
		return nil, errors.New("CRX header extends beyond archive")
	}
	if !bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 3, 4}) {
		return nil, errors.New("CRX ZIP payload is missing")
	}
	return data[offset:], nil
}

// relativePath applies both Windows and Unix constraints regardless of the host OS.
func relativePath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe extension path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe extension path %q", name)
		}
	}
	clean := path.Clean(name)
	if clean == "." || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("unsafe extension path %q", name)
	}
	return filepath.FromSlash(clean), nil
}

func cachedDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("extension cache is not a real directory: %s", dir)
	}
	_, err = readManifest(dir)
	return err
}

func (m *Manager) unpack(data []byte, id string) (string, error) {
	if rel, err := relativePath(id); err != nil || filepath.Base(rel) != rel {
		return "", errors.New("unsafe extension cache ID")
	}
	target := filepath.Join(m.root, id)
	if _, err := os.Lstat(target); err == nil {
		return target, cachedDirectory(target)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	payload, err := zipPayload(data)
	if err != nil {
		return "", err
	}
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return "", fmt.Errorf("open ZIP archive: %w", err)
	}
	if len(archive.File) > maxArchiveEntries {
		return "", errors.New("extension archive contains too many entries")
	}
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(m.root, ".unpacking-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var total int64
	for _, entry := range archive.File {
		name, err := relativePath(entry.Name)
		if err != nil {
			return "", err
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return "", fmt.Errorf("unsupported ZIP entry type: %s", entry.Name)
		}
		destination := filepath.Join(tmp, name)
		if mode.IsDir() {
			if err := os.MkdirAll(destination, 0755); err != nil {
				return "", err
			}
			continue
		}
		if entry.UncompressedSize64 > uint64(maxUnpackedBytes-total) {
			return "", errors.New("unpacked extension exceeds size limit")
		}
		written, err := extractFile(entry, destination, maxUnpackedBytes-total)
		if err != nil {
			return "", fmt.Errorf("extract %s: %w", entry.Name, err)
		}
		total += written
	}
	if _, err := readManifest(tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		// Another manager may have published the same immutable cache entry.
		if cachedDirectory(target) == nil {
			return target, nil
		}
		return "", fmt.Errorf("publish unpacked extension: %w", err)
	}
	return target, nil
}

func extractFile(entry *zip.File, destination string, remaining int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return 0, err
	}
	source, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer source.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, io.LimitReader(source, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if written > remaining {
		return written, errors.New("unpacked extension exceeds size limit")
	}
	return written, closeErr
}

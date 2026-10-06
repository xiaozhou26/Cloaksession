package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// MZAR v1/v2: magic, u16 BE version, 16-byte salt, 12-byte nonce,
// AES-256-GCM ciphertext and tag; scrypt N=16384, r=8, p=1, no AAD.
type archiveFile struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}
type archiveExtension struct {
	ID      string        `json:"id"`
	Version string        `json:"version"`
	Files   []archiveFile `json:"files"`
}
type archiveManifest struct {
	Magic      string             `json:"magic"`
	Version    uint16             `json:"version"`
	Profile    map[string]any     `json:"profile"`
	Files      []archiveFile      `json:"files"`
	Extensions []archiveExtension `json:"extensions,omitempty"`
}

func archiveCipher(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, 16384, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func encryptArchive(plain []byte, passphrase string) ([]byte, error) {
	out := make([]byte, 34)
	copy(out, "MZAR")
	binary.BigEndian.PutUint16(out[4:6], 2)
	if _, err := rand.Read(out[6:]); err != nil {
		return nil, err
	}
	aead, err := archiveCipher(passphrase, out[6:22])
	if err != nil {
		return nil, err
	}
	return aead.Seal(out, out[22:34], plain, nil), nil
}
func decryptArchive(buf []byte, passphrase string) ([]byte, error) {
	if len(buf) < 50 {
		return nil, errors.New("File too small")
	}
	if string(buf[:4]) != "MZAR" {
		return nil, errors.New("Not a Cloaksession archive")
	}
	version := binary.BigEndian.Uint16(buf[4:6])
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("Unsupported archive version %d", version)
	}
	aead, err := archiveCipher(passphrase, buf[6:22])
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, buf[22:34], buf[34:], nil)
	if err != nil {
		return nil, errors.New("Wrong passphrase or corrupted archive.")
	}
	return plain, nil
}
func putChunk(out *bytes.Buffer, b []byte) error {
	if uint64(len(b)) > math.MaxUint32 {
		return errors.New("archive chunk exceeds 4 GiB")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(b)))
	out.Write(size[:])
	out.Write(b)
	return nil
}
func readChunk(plain *[]byte) ([]byte, error) {
	b := *plain
	if len(b) < 4 {
		return nil, errors.New("Corrupted archive (length truncated)")
	}
	n := uint64(binary.BigEndian.Uint32(b[:4]))
	if n > uint64(len(b)-4) {
		return nil, errors.New("Corrupted archive (content truncated)")
	}
	out := b[4 : 4+int(n)]
	*plain = b[4+int(n):]
	return out, nil
}
func collectFiles(root string) ([]archiveFile, [][]byte, error) {
	metas := []archiveFile{}
	contents := [][]byte{}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return metas, contents, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("archive root is not a directory: %s", root)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot archive symlink: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("cannot archive special file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err = validateArchivePath(rel); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		metas = append(metas, archiveFile{Path: rel, Size: uint64(len(data)), SHA256: hex.EncodeToString(sum[:])})
		contents = append(contents, data)
		return nil
	})
	return metas, contents, err
}
func (s *Store) ExportArchive(id, passphrase, path string) error {
	if len(passphrase) < 8 {
		return errors.New("Passphrase must be at least 8 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := getProfile(s.db, id)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("not_found")
	}
	files, contents, err := collectFiles(text(p["dataDir"]))
	if err != nil {
		return err
	}
	manifest := archiveManifest{Magic: "MZAR", Version: 2, Profile: p, Files: files}
	if exts, ok := p["extensions"].([]any); ok {
		for _, v := range exts {
			ext, _ := v.(map[string]any)
			if ext["scope"] != "shared" {
				continue
			}
			dir := text(ext["dir"])
			if _, err = os.Stat(dir); errors.Is(err, os.ErrNotExist) {
				continue
			}
			metas, data, err := collectFiles(dir)
			if err != nil {
				return err
			}
			manifest.Extensions = append(manifest.Extensions, archiveExtension{ID: text(ext["id"]), Version: text(ext["version"]), Files: metas})
			contents = append(contents, data...)
		}
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	var plain bytes.Buffer
	if err = putChunk(&plain, b); err != nil {
		return err
	}
	for _, data := range contents {
		if err = putChunk(&plain, data); err != nil {
			return err
		}
	}
	encrypted, err := encryptArchive(plain.Bytes(), passphrase)
	if err != nil {
		return err
	}
	return atomicWrite(path, encrypted)
}

func safeSegment(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for i, c := range []byte(s) {
		alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alpha && (i == 0 || c != '.' && c != '_' && c != '-') {
			return false
		}
	}
	return true
}
func validateArchivePath(path string) error {
	if path == "" || strings.ContainsAny(path, "\\:\x00") || strings.HasPrefix(path, "/") || filepath.IsAbs(path) {
		return fmt.Errorf("path traversal: %q", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path traversal: %q", path)
		}
	}
	return nil
}

// ensureDirectory rejects symlink ancestors before any archive writes.
func ensureDirectory(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err = ensureDirectory(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe archive directory: %s", path)
	}
	return nil
}
func writeGuarded(root, path string, data []byte) error {
	if err := validateArchivePath(path); err != nil {
		return err
	}
	dest := filepath.Join(root, filepath.FromSlash(path))
	if err := ensureDirectory(filepath.Dir(dest)); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func verifyFiles(files []archiveFile, plain *[]byte) ([][]byte, error) {
	out := make([][]byte, 0, len(files))
	seen := map[string]bool{}
	for _, meta := range files {
		if err := validateArchivePath(meta.Path); err != nil {
			return nil, err
		}
		if seen[meta.Path] {
			return nil, fmt.Errorf("duplicate archive path: %s", meta.Path)
		}
		seen[meta.Path] = true
		data, err := readChunk(plain)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if uint64(len(data)) != meta.Size || hex.EncodeToString(sum[:]) != meta.SHA256 {
			return nil, fmt.Errorf("Checksum mismatch: %s", meta.Path)
		}
		out = append(out, data)
	}
	return out, nil
}
func (s *Store) ImportArchive(passphrase, path string) (string, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	plain, err := decryptArchive(buf, passphrase)
	if err != nil {
		return "", err
	}
	manifestJSON, err := readChunk(&plain)
	if err != nil {
		return "", err
	}
	var manifest archiveManifest
	if err = decodeJSON(manifestJSON, &manifest); err != nil {
		return "", fmt.Errorf("Corrupted archive (manifest JSON): %w", err)
	}
	if manifest.Magic != "MZAR" || (manifest.Version != 1 && manifest.Version != 2) {
		return "", errors.New("Corrupted archive (manifest header)")
	}
	p := manifest.Profile
	if p == nil {
		return "", errors.New("Corrupted archive (missing profile)")
	}
	if _, ok := p["chromixOptions"]; !ok {
		p["chromixOptions"] = map[string]any{}
	}
	if err = validateProfile(p); err != nil {
		return "", fmt.Errorf("Corrupted archive (profile): %w", err)
	}
	profileFiles, err := verifyFiles(manifest.Files, &plain)
	if err != nil {
		return "", err
	}
	extensionFiles := make([][][]byte, len(manifest.Extensions))
	for i, ext := range manifest.Extensions {
		extensionFiles[i], err = verifyFiles(ext.Files, &plain)
		if err != nil {
			return "", err
		}
	}
	if len(plain) != 0 {
		return "", errors.New("Corrupted archive (trailing content)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	profilesRoot := filepath.Join(s.DataDir, "profiles")
	extensionsRoot := filepath.Join(s.DataDir, "extensions")
	if err = ensureDirectory(profilesRoot); err != nil {
		return "", err
	}
	if err = ensureDirectory(extensionsRoot); err != nil {
		return "", err
	}
	id := text(p["id"])
	for {
		if !safeSegment(id) {
			id, err = newID()
			if err != nil {
				return "", err
			}
		}
		existing, err := getProfile(tx, id)
		if err != nil {
			return "", err
		}
		_, statErr := os.Lstat(filepath.Join(profilesRoot, id))
		if existing == nil && errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		id = ""
	}
	stage, err := os.MkdirTemp(profilesRoot, ".import-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	for i, meta := range manifest.Files {
		if err = writeGuarded(stage, meta.Path, profileFiles[i]); err != nil {
			return "", err
		}
	}
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.RemoveAll(created[i])
			}
		}
	}()
	sharedDirs := map[string]string{}
	for i, ext := range manifest.Extensions {
		if !safeSegment(ext.ID) {
			continue
		}
		version := ext.Version
		if !safeSegment(version) {
			version = "0"
		}
		parent := filepath.Join(extensionsRoot, ext.ID)
		if err = ensureDirectory(parent); err != nil {
			return "", err
		}
		dest := filepath.Join(parent, version)
		sharedDirs[ext.ID+"\x00"+ext.Version] = dest
		if info, e := os.Lstat(dest); e == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("unsafe extension directory: %s", dest)
			}
			continue
		} else if !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
		temp, err := os.MkdirTemp(parent, ".import-")
		if err != nil {
			return "", err
		}
		for j, meta := range ext.Files {
			if err = writeGuarded(temp, meta.Path, extensionFiles[i][j]); err != nil {
				os.RemoveAll(temp)
				return "", err
			}
		}
		if err = os.Rename(temp, dest); err != nil {
			os.RemoveAll(temp)
			return "", err
		}
		created = append(created, dest)
	}
	dest := filepath.Join(profilesRoot, id)
	oldDir := text(p["dataDir"])
	p["id"] = id
	p["dataDir"] = dest
	// Imported references must point at the restored files, not the exporting machine.
	if exts, ok := p["extensions"].([]any); ok {
		for _, v := range exts {
			ext := v.(map[string]any)
			if ext["scope"] == "shared" {
				if dir, ok := sharedDirs[text(ext["id"])+"\x00"+text(ext["version"])]; ok {
					ext["dir"] = dir
				}
			} else {
				old := strings.ReplaceAll(text(ext["dir"]), "\\", "/")
				base := strings.TrimRight(strings.ReplaceAll(oldDir, "\\", "/"), "/")
				if strings.HasPrefix(old, base+"/") {
					rel := strings.TrimPrefix(old, base+"/")
					if validateArchivePath(rel) == nil {
						ext["dir"] = filepath.Join(dest, filepath.FromSlash(rel))
					}
				}
			}
		}
	}
	if err = os.Rename(stage, dest); err != nil {
		return "", err
	}
	created = append(created, dest)
	if err = insertProfile(tx, p); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	committed = true
	return id, nil
}

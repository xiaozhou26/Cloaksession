package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var tokenMu sync.Mutex

// LoadOrCreateToken preserves the Rust mcp-token file's 64-hex wire format.
// Unix files are owner-readable/writable only; Windows uses the user's default ACL.
func LoadOrCreateToken(dataDir string) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, "mcp-token")
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("mcp-token must be a regular file")
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(path, 0600); err != nil {
				return "", err
			}
		}
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		existing := strings.TrimSpace(string(data))
		if len(data) <= 4096 && len(existing) == 64 {
			if _, err := hex.DecodeString(existing); err == nil {
				return existing, nil
			}
		}
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(entropy[:])
	file, err := os.CreateTemp(dataDir, ".mcp-token-*")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.WriteString(token); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temp, path); err != nil {
		return "", err
	}
	return token, nil
}

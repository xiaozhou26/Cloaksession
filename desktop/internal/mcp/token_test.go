package mcp

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrCreateToken(t *testing.T) {
	for _, existing := range []string{"", "invalid", strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("a", 64), " \n" + strings.Repeat("AB", 32) + "\n"} {
		t.Run(existing, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "mcp-token")
			if existing != "" {
				if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
					t.Fatal(err)
				}
			}
			token, err := LoadOrCreateToken(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(token) != 64 {
				t.Fatal(token)
			}
			if _, err := hex.DecodeString(token); err != nil {
				t.Fatal(err)
			}
			trimmed := strings.TrimSpace(existing)
			if len(trimmed) == 64 {
				if _, err := hex.DecodeString(trimmed); err == nil && token != trimmed {
					t.Fatal("valid token changed")
				}
			}
			second, err := LoadOrCreateToken(dir)
			if err != nil || token != second {
				t.Fatal("token not stable", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || strings.TrimSpace(string(data)) != token {
				t.Fatal("disk mismatch", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatalf("permissions %o", info.Mode().Perm())
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatal("temporary files left behind")
			}
		})
	}
	dir := filepath.Join(t.TempDir(), "nested", "data")
	if _, err := LoadOrCreateToken(dir); err != nil {
		t.Fatal(err)
	}
}

func TestTokenConcurrentCreation(t *testing.T) {
	dir := t.TempDir()
	results := make(chan string, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := LoadOrCreateToken(dir)
			if err != nil {
				t.Error(err)
			}
			results <- token
		}()
	}
	wg.Wait()
	close(results)
	var first string
	for token := range results {
		if first == "" {
			first = token
		}
		if token != first {
			t.Fatal("concurrent tokens differ")
		}
	}
}

func TestTokenFilesystemErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-token")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateToken(dir); err == nil {
		t.Fatal("directory accepted as token")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		target := filepath.Join(dir, "secret")
		if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateToken(dir); err == nil {
			t.Fatal("symlink accepted")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "untouched" {
			t.Fatal("symlink target modified")
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateToken(file); err == nil {
		t.Fatal("file accepted as data directory")
	}
}

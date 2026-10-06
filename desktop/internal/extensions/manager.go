// Package extensions manages shared unpacked browser extensions and profile references.
package extensions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists extension references in the existing profile extensions field.
type Store interface {
	ProfileGet(string) (map[string]any, error)
	ProfileUpdate(string, map[string]any) (map[string]any, error)
	ProfilesList() ([]map[string]any, error)
}

type Manager struct {
	root    string
	store   Store
	client  *http.Client
	mu      sync.Mutex
	filesMu sync.Mutex
}

func New(dataDir string, store Store) *Manager {
	root := filepath.Join(dataDir, "extensions")
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &Manager{
		root: root, store: store,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout: 15 * time.Second,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 5 {
					return errors.New("too many Web Store redirects")
				}
				if req.URL.Scheme != "https" {
					return errors.New("insecure Web Store redirect")
				}
				return nil
			},
		},
	}
}

func (m *Manager) List(profileID string) ([]map[string]any, error) {
	if m.store == nil {
		return nil, errors.New("profile store is unavailable")
	}
	profile, err := m.store.ProfileGet(profileID)
	if err != nil {
		return nil, err
	}
	return extensionList(profile["extensions"])
}

func extensionList(value any) ([]map[string]any, error) {
	result := make([]map[string]any, 0)
	if value == nil {
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid profile extensions: %w", err)
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid profile extensions: %w", err)
	}
	if result == nil {
		result = make([]map[string]any, 0)
	}
	for _, ext := range result {
		if ext == nil {
			return nil, errors.New("invalid null extension entry")
		}
	}
	return result, nil
}

// StoreEntries returns unique references across profiles, not unreferenced cache directories.
func (m *Manager) StoreEntries() ([]map[string]any, error) {
	if m.store == nil {
		return nil, errors.New("profile store is unavailable")
	}
	profiles, err := m.store.ProfilesList()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	seen := make(map[string]bool)
	for _, profile := range profiles {
		exts, err := m.List(text(profile, "id"))
		if err != nil {
			return nil, err
		}
		for _, ext := range exts {
			id := text(ext, "id")
			if !seen[id] {
				seen[id] = true
				result = append(result, ext)
			}
		}
	}
	return result, nil
}

// SweepOrphans removes unreferenced cache directories and interrupted unpacks.
// Call it at startup, before preparing extensions that have no profile references yet.
// Symlink entries are left untouched; a symlinked extensions root is rejected.
func (m *Manager) SweepOrphans() error {
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()

	info, err := os.Lstat(m.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("extension cache is not a real directory: %s", m.root)
	}
	if m.store == nil {
		return errors.New("profile store is unavailable")
	}
	profiles, err := m.store.ProfilesList()
	if err != nil {
		return err
	}
	referenced := make(map[string]bool)
	for _, profile := range profiles {
		exts, err := m.List(text(profile, "id"))
		if err != nil {
			return err
		}
		for _, ext := range exts {
			if dir := text(ext, "dir"); dir != "" {
				// Keep the legacy basename matching, including external folder references.
				referenced[filepath.Base(dir)] = true
			}
		}
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if referenced[name] && !strings.HasSuffix(name, ".tmp_unpacked") {
			continue
		}
		// Names come only from ReadDir; RemoveAll does not follow nested symlinks.
		if err := os.RemoveAll(filepath.Join(m.root, name)); err != nil {
			failures = append(failures, fmt.Errorf("remove extension cache %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) update(profileID string, change func([]map[string]any) []map[string]any) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	exts, err := m.List(profileID)
	if err != nil {
		return nil, err
	}
	profile, err := m.store.ProfileUpdate(profileID, map[string]any{"extensions": change(exts)})
	if err != nil {
		return nil, err
	}
	return extensionList(profile["extensions"])
}

func (m *Manager) Add(profileID string, extension map[string]any) ([]map[string]any, error) {
	if text(extension, "id") == "" || text(extension, "dir") == "" {
		return nil, errors.New("extension id and dir are required")
	}
	copies, err := extensionList([]map[string]any{extension})
	if err != nil {
		return nil, err
	}
	ext := copies[0]
	for key, value := range map[string]any{"name": text(ext, "id"), "version": "", "enabled": true, "scope": "shared", "source": "folder"} {
		if _, ok := ext[key]; !ok {
			ext[key] = value
		}
	}
	return m.update(profileID, func(exts []map[string]any) []map[string]any {
		result := make([]map[string]any, 0, len(exts)+1)
		for _, existing := range exts {
			if text(existing, "id") != text(ext, "id") {
				result = append(result, existing)
			}
		}
		return append(result, ext)
	})
}

func (m *Manager) Remove(profileID, extID string) ([]map[string]any, error) {
	return m.update(profileID, func(exts []map[string]any) []map[string]any {
		result := make([]map[string]any, 0, len(exts))
		for _, ext := range exts {
			if text(ext, "id") != extID {
				result = append(result, ext)
			}
		}
		return result
	})
}

func (m *Manager) Toggle(profileID, extID string, enabled bool) ([]map[string]any, error) {
	return m.update(profileID, func(exts []map[string]any) []map[string]any {
		for _, ext := range exts {
			if text(ext, "id") == extID {
				ext["enabled"] = enabled
			}
		}
		return exts
	})
}

func (m *Manager) PrepareFromFile(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read extension file: %w", err)
	}
	defer file.Close()
	data, err := readLimited(file, maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("read extension file: %w", err)
	}
	id := shortHash(data)
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	dir, err := m.unpack(data, id)
	if err != nil {
		return nil, err
	}
	return buildConfig("file", id, dir)
}

func (m *Manager) PrepareFromFolder(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	dir, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	manifest, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	// Preserve Rust's folder IDs, including its literal, lowercased manifest key.
	id := strings.ToLower(text(manifest, "key"))
	if id == "" {
		id = shortHash([]byte(dir))
	}
	return configFromManifest("folder", id, dir, manifest), nil
}

func (m *Manager) PrepareFromWebStore(ctx context.Context, urlOrID string) (map[string]any, error) {
	id, err := parseWebStoreID(urlOrID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := filepath.Join(m.root, id)
	if _, err := os.Lstat(dir); err == nil {
		if err := cachedDirectory(dir); err != nil {
			return nil, err
		}
		return buildConfig("web-store", id, dir)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	data, err := m.download(ctx, id)
	if err != nil {
		return nil, err
	}
	dir, err = m.unpack(data, id)
	if err != nil {
		return nil, err
	}
	return buildConfig("web-store", id, dir)
}

func parseWebStoreID(input string) (string, error) {
	id := strings.TrimSpace(input)
	if strings.HasPrefix(id, "http") {
		u, err := url.Parse(id)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("could not parse a 32-char extension ID from: %s", input)
		}
		id = u.Path[strings.LastIndex(u.Path, "/")+1:]
	}
	if len(id) == 32 {
		valid := true
		for _, c := range id {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				valid = false
				break
			}
		}
		if valid {
			return strings.ToLower(id), nil
		}
	}
	return "", fmt.Errorf("could not parse a 32-char extension ID from: %s", input)
}

func (m *Manager) download(ctx context.Context, id string) ([]byte, error) {
	query := url.Values{
		"response": {"redirect"}, "acceptformat": {"crx2,crx3"}, "prodversion": {"131.0"},
		"x": {"id=" + id + "&installsource=ondemand&uc"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://clients2.google.com/service/update2/crx?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131.0.0.0 Safari/537.36")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Web Store download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, unavailable(id)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Web Store returned HTTP %d for extension %s", resp.StatusCode, id)
	}
	data, err := readLimited(resp.Body, maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("read CRX body: %w", err)
	}
	if len(data) < 16 {
		return nil, unavailable(id)
	}
	return data, nil
}

func unavailable(id string) error {
	return fmt.Errorf("extension %s isn't available from the Web Store (it may be delisted or region-restricted); try uploading the .crx/.zip instead", id)
}

func shortHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16])
}

func text(value map[string]any, key string) string {
	s, _ := value[key].(string)
	return s
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("extension data exceeds %d byte limit", limit)
	}
	return data, nil
}

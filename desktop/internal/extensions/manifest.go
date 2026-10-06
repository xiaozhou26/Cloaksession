package extensions

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxManifestBytes = 4 << 20
const maxIconBytes = 16 << 20

func readResource(dir, name string, limit int64) ([]byte, error) {
	rel, err := relativePath(name)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}
	inside, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(inside) {
		return nil, fmt.Errorf("extension resource escapes directory: %s", name)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file, limit)
}

func readManifest(dir string) (map[string]any, error) {
	data, err := readResource(dir, "manifest.json", maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read extension manifest: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid extension manifest: %w", err)
	}
	if manifest == nil {
		return nil, fmt.Errorf("invalid extension manifest: expected object")
	}
	return manifest, nil
}

func buildConfig(source, id, dir string) (map[string]any, error) {
	manifest, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	return configFromManifest(source, id, dir, manifest), nil
}

func configFromManifest(source, id, dir string, manifest map[string]any) map[string]any {
	name := text(manifest, "name")
	if strings.HasPrefix(name, "__MSG_") && strings.HasSuffix(name, "__") {
		key := strings.TrimSuffix(strings.TrimPrefix(name, "__MSG_"), "__")
		name = localizedName(dir, text(manifest, "default_locale"), key)
	}
	if name == "" || strings.HasPrefix(name, "__MSG_") {
		name = id
	}
	return map[string]any{
		"id": id, "name": name, "version": text(manifest, "version"),
		"enabled": true, "scope": "shared", "dir": dir, "source": source,
	}
}

func localizedName(dir, locale, key string) string {
	for _, candidate := range []string{locale, "en", "en_US"} {
		if candidate == "" || strings.ContainsAny(candidate, "/\\:") {
			continue
		}
		data, err := readResource(dir, "_locales/"+candidate+"/messages.json", maxManifestBytes)
		if err != nil {
			continue
		}
		var messages map[string]struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &messages) != nil {
			continue
		}
		if entry, ok := messages[key]; ok && entry.Message != "" {
			return entry.Message
		}
		for messageKey, entry := range messages {
			if strings.EqualFold(messageKey, key) && entry.Message != "" {
				return entry.Message
			}
		}
	}
	return ""
}

// Icon returns a data URL, or nil when no safe, readable icon is available.
// profileID is retained for frontend compatibility; dir already resolves the extension.
func (m *Manager) Icon(extension map[string]any, profileID *string) (any, error) {
	dir := text(extension, "dir")
	if dir == "" {
		return nil, nil
	}
	manifest, err := readManifest(dir)
	if err != nil {
		return nil, nil
	}
	icons := manifest["icons"]
	if icons == nil {
		for _, key := range []string{"action", "browser_action", "page_action"} {
			if action, ok := manifest[key].(map[string]any); ok && action["default_icon"] != nil {
				icons = action["default_icon"]
				break
			}
		}
	}
	type candidate struct {
		size int
		path string
	}
	var candidates []candidate
	switch value := icons.(type) {
	case string:
		candidates = append(candidates, candidate{path: value})
	case map[string]any:
		for key, value := range value {
			if name, ok := value.(string); ok {
				size, _ := strconv.Atoi(key)
				candidates = append(candidates, candidate{size, name})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].size == candidates[j].size {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].size > candidates[j].size
	})
	for _, icon := range candidates {
		data, err := readResource(dir, icon.path, maxIconBytes)
		if err != nil {
			continue
		}
		mime := "image/png"
		switch strings.ToLower(filepath.Ext(icon.path)) {
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".svg":
			mime = "image/svg+xml"
		case ".gif":
			mime = "image/gif"
		case ".webp":
			mime = "image/webp"
		case ".ico":
			mime = "image/x-icon"
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return nil, nil
}

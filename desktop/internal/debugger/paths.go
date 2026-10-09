package debugger

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func regular(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	return path, nil
}
func nodeExecutable(configured string) (string, error) {
	if configured == "" {
		configured = "node"
	}
	if !filepath.IsAbs(configured) && configured != "node" && configured != "node.exe" {
		return "", errors.New("nodePath must be an absolute Node.js executable path")
	}
	path := configured
	var err error
	if !filepath.IsAbs(path) {
		path, err = exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("Node.js is unavailable; configure nodePath: %w", err)
		}
	}
	path, err = regular(path)
	if err != nil {
		return "", err
	}
	base := strings.ToLower(filepath.Base(path))
	if runtime.GOOS == "windows" && base != "node.exe" || runtime.GOOS != "windows" && base != "node" && base != "nodejs" {
		return "", errors.New("nodePath must point to a Node.js binary, not a shell or wrapper")
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", errors.New("Node.js binary is not executable")
	}
	return path, nil
}
func bridgePath(resourceDir string) (string, error) {
	root, err := filepath.Abs(filepath.Join(resourceDir, "reverse"))
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("reverse resources unavailable: %w", err)
	}
	bridge, err := regular(filepath.Join(root, "bridge.mjs"))
	if err != nil || !inside(root, bridge) {
		return "", errors.New("invalid reverse bridge path")
	}
	pkg := filepath.Join(root, "node_modules", "js-reverse-mcp")
	manifest, err := regular(filepath.Join(pkg, "package.json"))
	if err != nil || !inside(root, manifest) {
		return "", errors.New("js-reverse-mcp package missing or outside reverse resources")
	}
	info, err := os.Stat(manifest)
	if err != nil || info.Size() > 1<<20 {
		return "", errors.New("invalid reverse package manifest")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return "", err
	}
	var meta struct {
		Name    string          `json:"name"`
		Version string          `json:"version"`
		Bin     json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &meta) != nil || meta.Name != "js-reverse-mcp" || meta.Version != "4.0.5" {
		return "", errors.New("js-reverse-mcp must be pinned to version 4.0.5")
	}
	var bin string
	if json.Unmarshal(meta.Bin, &bin) != nil {
		var bins map[string]string
		if json.Unmarshal(meta.Bin, &bins) != nil {
			return "", errors.New("invalid reverse package CLI")
		}
		bin = bins["js-reverse-mcp"]
	}
	if bin == "" || filepath.IsAbs(bin) {
		return "", errors.New("invalid reverse package CLI")
	}
	cli, err := regular(filepath.Join(pkg, bin))
	if err != nil || !inside(pkg, cli) || !inside(root, cli) {
		return "", errors.New("reverse package CLI missing or outside package")
	}
	return bridge, nil
}
func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("invalid managed CDP endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	port, e := strconv.Atoi(u.Port())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || e != nil || port < 1 || port > 65535 || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("managed CDP endpoint must be loopback HTTP with an explicit port")
	}
	return nil
}
func safeEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "NODE_OPTIONS", "NODE_PATH", "NODE_EXTRA_CA_CERTS":
			continue
		}
		env = append(env, entry)
	}
	return env
}

// confinedPath also resolves existing ancestors of a new output file.
func confinedPath(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("file path must not be empty")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !inside(root, path) {
		return "", errors.New("file path is outside the debug allowedRoot")
	}
	ancestor := path
	suffix := []string{}
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", errors.New("invalid file path")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	if !inside(root, resolved) {
		return "", errors.New("file path resolves outside the debug allowedRoot")
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.TrimPrefix(resolved, filepath.VolumeName(resolved)), ":") {
		return "", errors.New("alternate data streams are not supported")
	}
	return resolved, nil
}

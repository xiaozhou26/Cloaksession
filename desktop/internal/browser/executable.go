package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// resolveNodeExecutable uses the child environment without changing the app's PATH.
func resolveNodeExecutable(configured string, env []string) (string, error) {
	home, _ := os.UserHomeDir()
	return resolveNodeExecutableWith(configured, env, runtime.GOOS, home, func(path string) (string, error) {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("%s is not an executable file", path)
		}
		return filepath.Abs(path)
	})
}

func resolveNodeExecutableWith(configured string, env []string, goos, home string, lookup func(string) (string, error)) (string, error) {
	if configured == "" {
		configured = "node"
	}
	failure := func(err error) (string, error) {
		return "", fmt.Errorf("cannot resolve Node.js executable %q: %w; install Node.js 20+ or set chromix.nodePath to an absolute Node.js executable path", configured, err)
	}
	check := func(path string) (string, error) {
		resolved, err := lookup(path)
		if err == nil || goos != "windows" || filepath.Ext(path) != "" {
			return resolved, err
		}
		extensions := executableEnv(env, "PATHEXT", goos)
		if extensions == "" {
			extensions = ".COM;.EXE;.BAT;.CMD"
		}
		for _, extension := range strings.Split(extensions, ";") {
			if strings.HasPrefix(extension, ".") {
				if resolved, err = lookup(path + strings.ToLower(extension)); err == nil {
					return resolved, nil
				}
			}
		}
		return "", err
	}
	path := configured
	if path == "~" || strings.HasPrefix(path, "~/") || goos == "windows" && strings.HasPrefix(path, `~\`) {
		if home == "" {
			return failure(fmt.Errorf("cannot determine the home directory for tilde expansion"))
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
	}
	explicit := filepath.IsAbs(path) || strings.ContainsRune(configured, '/') || goos == "windows" && strings.ContainsAny(configured, `\:`)
	if explicit || configured == "~" {
		resolved, err := check(path)
		if err != nil {
			return failure(err)
		}
		return resolved, nil
	}
	separator := ":"
	if goos == "windows" {
		separator = ";"
	}
	for _, dir := range strings.Split(executableEnv(env, "PATH", goos), separator) {
		if goos == "windows" {
			dir = strings.Trim(dir, `"`)
		}
		if dir == "" {
			continue
		}
		if resolved, err := check(filepath.Join(dir, path)); err == nil {
			if !filepath.IsAbs(dir) {
				return failure(exec.ErrDot)
			}
			return resolved, nil
		}
	}
	// Only the default Node name may fall back to a different installation.
	if configured == "node" || goos == "windows" && strings.EqualFold(configured, "node.exe") {
		for _, candidate := range nodeExecutableCandidates(goos, home, env) {
			if resolved, err := check(candidate); err == nil {
				return resolved, nil
			}
		}
	}
	return failure(exec.ErrNotFound)
}

func executableEnv(env []string, key, goos string) string {
	// exec.Cmd keeps the last duplicate; Windows environment keys ignore case.
	for i := len(env) - 1; i >= 0; i-- {
		name, value, ok := strings.Cut(env[i], "=")
		if ok && (name == key || goos == "windows" && strings.EqualFold(name, key)) {
			return value
		}
	}
	return ""
}

func nodeExecutableCandidates(goos, home string, env []string) []string {
	var paths []string
	switch goos {
	case "windows":
		for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if dir := executableEnv(env, key, goos); dir != "" {
				paths = append(paths, filepath.Join(dir, "nodejs", "node.exe"))
			}
		}
		return paths
	case "darwin":
		paths = []string{"/opt/homebrew/bin/node", "/usr/local/bin/node", "/usr/bin/node"}
	default:
		paths = []string{"/usr/local/bin/node", "/usr/bin/node", "/bin/node"}
	}
	if home != "" {
		for _, dir := range []string{".volta/bin", ".asdf/shims", ".local/share/mise/shims", ".local/bin", ".nvm/current/bin"} {
			paths = append(paths, filepath.Join(home, dir, "node"))
		}
	}
	return paths
}

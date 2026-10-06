package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestResolveNodeExecutableStrategy(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if runtime.GOOS == "windows" {
		// A UNC path stays absolute without a drive colon in simulated Unix PATHs.
		home = filepath.Join(`\\test-server\share`, "home")
	}
	first, second := filepath.Join(home, "first"), filepath.Join(home, "second")
	for _, tt := range []struct {
		name, configured, goos string
		env                    []string
		available              []string
		want                   string
		wantErr                bool
	}{
		{name: "Finder Homebrew", goos: "darwin", env: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}, available: []string{"/opt/homebrew/bin/node"}, want: "/opt/homebrew/bin/node"},
		{name: "Intel Homebrew", goos: "darwin", available: []string{"/usr/local/bin/node"}, want: "/usr/local/bin/node"},
		{name: "PATH before fallback", goos: "darwin", env: []string{"PATH=" + first}, available: []string{filepath.Join(first, "node"), "/opt/homebrew/bin/node"}, want: filepath.Join(first, "node")},
		{name: "PATH order", goos: "linux", env: []string{"PATH=" + first + ":" + second}, available: []string{filepath.Join(first, "node"), filepath.Join(second, "node")}, want: filepath.Join(first, "node")},
		{name: "environment override", goos: "linux", env: []string{"PATH=" + first, "PATH=" + second}, available: []string{filepath.Join(first, "node"), filepath.Join(second, "node")}, want: filepath.Join(second, "node")},
		{name: "empty override", goos: "linux", env: []string{"PATH=" + first, "PATH="}, available: []string{filepath.Join(first, "node")}, wantErr: true},
		{name: "Linux local", goos: "linux", available: []string{"/usr/local/bin/node"}, want: "/usr/local/bin/node"},
		{name: "Linux system", goos: "linux", available: []string{"/usr/bin/node"}, want: "/usr/bin/node"},
		{name: "user manager", goos: "darwin", available: []string{filepath.Join(home, ".volta/bin/node")}, want: filepath.Join(home, ".volta/bin/node")},
		{name: "explicit path with spaces", configured: filepath.Join(home, "my node"), goos: "darwin", available: []string{filepath.Join(home, "my node"), "/opt/homebrew/bin/node"}, want: filepath.Join(home, "my node")},
		{name: "invalid explicit never falls back", configured: filepath.Join(home, "missing"), goos: "darwin", available: []string{"/opt/homebrew/bin/node"}, wantErr: true},
		{name: "tilde", configured: "~/bin/node", goos: "darwin", available: []string{filepath.Join(home, "bin/node")}, want: filepath.Join(home, "bin/node")},
		{name: "custom bare name", configured: "node-custom", goos: "linux", env: []string{"PATH=" + first}, available: []string{filepath.Join(first, "node-custom")}, want: filepath.Join(first, "node-custom")},
		{name: "missing custom never falls back", configured: "node-custom", goos: "linux", available: []string{"/usr/bin/node"}, wantErr: true},
		{name: "unavailable", goos: "darwin", wantErr: true},
		{name: "Windows ProgramFiles", goos: "windows", env: []string{"ProgramFiles=" + first}, available: []string{filepath.Join(first, "nodejs/node.exe")}, want: filepath.Join(first, "nodejs/node.exe")},
		{name: "Windows local app data", goos: "windows", env: []string{"LOCALAPPDATA=" + first}, available: []string{filepath.Join(first, "nodejs/node.exe")}, want: filepath.Join(first, "nodejs/node.exe")},
		{name: "Windows PATH override and PATHEXT", goos: "windows", env: []string{"PATH=" + first, "Path=" + second, "PATHEXT=.EXE"}, available: []string{filepath.Join(first, "node.exe"), filepath.Join(second, "node.exe")}, want: filepath.Join(second, "node.exe")},
		{name: "Windows PATH order", goos: "windows", env: []string{"Path=" + first + ";" + second}, available: []string{filepath.Join(second, "node.exe")}, want: filepath.Join(second, "node.exe")},
		{name: "Windows node.exe fallback", configured: "node.exe", goos: "windows", env: []string{"programfiles=" + first}, available: []string{filepath.Join(first, "nodejs/node.exe")}, want: filepath.Join(first, "nodejs/node.exe")},
		{name: "Windows invalid explicit", configured: `C:\missing\node.exe`, goos: "windows", env: []string{"ProgramFiles=" + first}, available: []string{filepath.Join(first, "nodejs/node.exe")}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(path string) (string, error) {
				for _, available := range tt.available {
					if path == available {
						return path, nil
					}
				}
				return "", os.ErrNotExist
			}
			got, err := resolveNodeExecutableWith(tt.configured, tt.env, tt.goos, home, lookup)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "set chromix.nodePath to an absolute Node.js executable path") {
					t.Fatalf("expected actionable error, got %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestResolveNodeExecutableExplicitDoesNotSearch(t *testing.T) {
	for _, configured := range []string{"./missing-node", "~/missing-node"} {
		var calls []string
		_, err := resolveNodeExecutableWith(configured, []string{"PATH=/bin"}, "darwin", "/home/test", func(path string) (string, error) {
			calls = append(calls, path)
			return "", os.ErrNotExist
		})
		if err == nil || len(calls) != 1 || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("explicit lookup must stop after failure: calls=%v err=%v", calls, err)
		}
	}
	_, err := resolveNodeExecutableWith("~/node", nil, "darwin", "", func(string) (string, error) {
		t.Fatal("lookup must not run without a home directory")
		return "", nil
	})
	if err == nil {
		t.Fatal("missing home directory accepted")
	}
}

func TestResolveNodeExecutableFiles(t *testing.T) {
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveNodeExecutable(helper, []string{"PATH="}); err != nil || got != helper {
		t.Fatalf("explicit test helper: got %q, %v", got, err)
	}
	dir := t.TempDir()
	if _, err := resolveNodeExecutable(dir, nil); err == nil {
		t.Fatal("directory accepted as executable")
	}
	if runtime.GOOS == "windows" {
		return
	}
	path := filepath.Join(dir, "node")
	if err := os.WriteFile(path, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveNodeExecutable(path, nil); err == nil {
		t.Fatal("non-executable file accepted")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=/missing", "PATH=" + dir}
	original := append([]string(nil), env...)
	if got, err := resolveNodeExecutable("node", env); err != nil || got != path {
		t.Fatalf("effective PATH: got %q, %v", got, err)
	}
	if !reflect.DeepEqual(env, original) {
		t.Fatal("resolver changed child environment")
	}
	if err := os.Symlink(path, filepath.Join(dir, "linked-node")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveNodeExecutable(filepath.Join(dir, "linked-node"), nil); err != nil {
		t.Fatalf("executable symlink rejected: %v", err)
	}
}

func TestResolveNodeExecutableRejectsImplicitRelativePATH(t *testing.T) {
	_, err := resolveNodeExecutableWith("node", []string{"PATH=relative/bin"}, "linux", "", func(path string) (string, error) {
		return path, nil
	})
	if !errors.Is(err, exec.ErrDot) {
		t.Fatalf("expected exec.ErrDot, got %v", err)
	}
}

func TestPlaywrightNodeEnvironmentPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a Node symlink")
	}
	resources := nodeFixture(t, fakeBridge)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(node, filepath.Join(dir, "node")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/missing-parent-path")
	endpoint, _, _ := fakeCDP(t)
	m := New(resources, nil)
	defer m.Shutdown()
	settings := object{"browserEngine": "chromix", "chromix": object{"environment": object{"PATH": dir, "TEST_WS": "ws" + strings.TrimPrefix(endpoint, "http") + "/ws"}}}
	if _, err := m.Launch(object{"id": "node-env", "dataDir": t.TempDir()}, settings, ""); err != nil {
		t.Fatal(err)
	}
	h, err := m.get("node-env")
	if err != nil {
		t.Fatal(err)
	}
	if h.cmd.Path != filepath.Join(dir, "node") || executableEnv(h.cmd.Env, "PATH", runtime.GOOS) != dir {
		t.Fatalf("launch did not use overridden PATH: %s, %v", h.cmd.Path, h.cmd.Env)
	}
	if os.Getenv("PATH") != "/missing-parent-path" {
		t.Fatal("launch changed the parent PATH")
	}
}

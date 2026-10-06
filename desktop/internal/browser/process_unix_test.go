//go:build !windows

package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSingletonLocks(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, target string
		allowed      bool
	}{{"active", fmt.Sprintf("%s-%d", host, os.Getpid()), false}, {"remote", "other-host-123", false}, {"stale", host + "-2147483647", true}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
				if err := os.Symlink(test.target, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			err := cleanSingletonLocks(dir)
			if (err == nil) != test.allowed {
				t.Fatal(err)
			}
			_, err = os.Lstat(filepath.Join(dir, "SingletonLock"))
			if test.allowed && !os.IsNotExist(err) {
				t.Fatal("stale lock survived")
			}
			if !test.allowed && err != nil {
				t.Fatal("active lock removed", err)
			}
		})
	}
}

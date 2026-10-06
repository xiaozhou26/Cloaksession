//go:build !windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	}
}

func cleanSingletonLocks(dir string) error {
	lock := filepath.Join(dir, "SingletonLock")
	target, err := os.Readlink(lock)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot inspect browser singleton lock: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	index := strings.LastIndex(target, "-")
	if index < 0 || target[:index] != host {
		return fmt.Errorf("browser data directory is locked by %s", target)
	}
	pid, err := strconv.Atoi(target[index+1:])
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid browser singleton lock %q", target)
	}
	if err = syscall.Kill(pid, 0); err != syscall.ESRCH {
		return fmt.Errorf("browser data directory is already in use by pid %d", pid)
	}
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

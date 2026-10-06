//go:build windows

package browser

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func terminateProcess(cmd *exec.Cmd) { killProcess(cmd) }
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		command := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		configureProcess(command)
		_ = command.Run()
		_ = cmd.Process.Kill()
	}
}

func cleanSingletonLocks(string) error { return nil }

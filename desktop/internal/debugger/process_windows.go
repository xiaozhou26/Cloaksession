//go:build windows

package debugger

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
func killProcess(cmd *exec.Cmd) {
	// The bridge imports the CLI in this process; no shell or browser is owned here.
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

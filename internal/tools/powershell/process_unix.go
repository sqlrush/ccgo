//go:build !windows

package powershelltools

import (
	"errors"
	"os/exec"
	"syscall"
)

func configurePowerShellCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killPowerShellProcessGroup(cmd)
	}
}

func killPowerShellProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

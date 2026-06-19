//go:build !windows

package powershelltools

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

const processInterruptGrace = 200 * time.Millisecond

func configurePowerShellCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = processInterruptGrace
	cmd.Cancel = func() error {
		return signalPowerShellProcessGroup(cmd, syscall.SIGTERM)
	}
}

func signalPowerShellProcessGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

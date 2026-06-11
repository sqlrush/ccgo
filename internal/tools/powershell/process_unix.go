//go:build !windows

package powershelltools

import (
	"os/exec"
	"syscall"
)

func configurePowerShellCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

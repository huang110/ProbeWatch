//go:build windows

package terminal

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// taskkill /T walks the child process tree (including powershell/cmd
	// descendants), while /F prevents a stubborn console process from keeping
	// the agent command slot occupied.
	taskkill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := taskkill.Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
	}
}

// killTree kills the child and its descendants.
// TODO(M2): replace taskkill with a Job Object (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE).
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

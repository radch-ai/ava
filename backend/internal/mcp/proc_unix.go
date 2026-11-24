//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
)

// setPgid puts the stdio server in its own process group so teardown can
// signal the server and any children it spawned.
func setPgid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup SIGKILLs the whole process group led by the command (negative
// pid), so grandchildren die with it. A command that never started is a
// no-op.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

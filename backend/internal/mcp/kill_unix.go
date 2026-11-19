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

// killGroup signals the whole process group led by pid (negative pid). A
// no-op for a zero pid — the process never started.
func killGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

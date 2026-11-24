//go:build !unix

package mcp

import "os/exec"

// Process groups do not exist here; the direct child is killed instead.

func setPgid(*exec.Cmd) {}

// killGroup kills the direct child. Grandchildren are the platform's
// concern; the transport close still bounds teardown.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

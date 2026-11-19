//go:build !unix

package mcp

import "os/exec"

// Process groups do not exist here; teardown still cancels the lifetime
// context, which kills the direct child.

func setPgid(*exec.Cmd) {}

func killGroup(int) {}

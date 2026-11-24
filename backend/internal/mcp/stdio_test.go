//go:build unix

package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperBin is the compiled testdata/mcpserver, built once per package run:
// the tests drive a real child process, not a stand-in.
var helperBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	helperBin = filepath.Join(dir, "mcpserver")
	if out, err := exec.Command("go", "build", "-o", helperBin, "./testdata/mcpserver").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build helper: %v\n%s\n", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// readPids waits for the helper's pidfile and returns the pids it listed.
func readPids(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			fields := strings.Fields(string(data))
			pids := make([]int, 0, len(fields))
			for _, field := range fields {
				pid, err := strconv.Atoi(field)
				if err != nil {
					t.Fatalf("pidfile %q unparsable: %v", data, err)
				}
				pids = append(pids, pid)
			}
			return pids
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pidfile %s never appeared", path)
	return nil
}

// processGone treats zombies as gone: the kill(0) probe still finds them,
// but a killed process that has not been reaped yet holds no resources.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(data)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] == 'Z'
	}
	return false
}

// waitPidsGone asserts every pid disappears within the deadline.
func waitPidsGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, pid := range pids {
			if !processGone(pid) {
				alive = true
				break
			}
		}
		if !alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, pid := range pids {
		stat, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		cmdline, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		t.Logf("pid %d stat=%q cmdline=%q", pid, string(stat), strings.ReplaceAll(string(cmdline), "\x00", " "))
	}
	t.Fatalf("processes %v still alive after teardown", pids)
}

// stdioConfig is the always-valid part of a helper connection.
func stdioConfig(t *testing.T, args ...string) ServerConfig {
	t.Helper()
	return ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      args,
		Timeout:   5 * time.Second,
	}
}

// syncOnceAndCall runs the discovery pass and one tool call on a stdio
// connection: the round trip every stdio test needs.
func syncOnceAndCall(t *testing.T, c *Client, remote, args string) string {
	t.Helper()
	listed, _, err := c.SyncStaged(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	c.Commit()
	found := false
	for _, tool := range listed {
		if tool.Remote == remote {
			found = true
		}
	}
	if !found {
		t.Fatalf("listing does not report %q: %+v", remote, listed)
	}
	inv, ok := c.InvokableByRemote(remote)
	if !ok {
		t.Fatalf("%s not cached after sync", remote)
	}
	out, err := inv.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("run %s: %v", remote, err)
	}
	return out
}

// A spawned server answers the handshake and its tools; teardown kills the
// process group, grandchildren included.
func TestStdioRoundTripKillsGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(t.Context(), stdioConfig(t, "-pidfile", pidfile, "-spawn-child"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if out := syncOnceAndCall(t, c, "echo", `{"text":"hello stdio"}`); out != "hello stdio" {
		t.Fatalf("echo result = %q", out)
	}

	// The over-long name is skipped with a reason, never renamed.
	_, skipped, err := c.SyncStaged(t.Context())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0].Remote, "xxxx") {
		t.Fatalf("skipped = %+v, want the over-long tool", skipped)
	}

	pids := readPids(t, pidfile)
	if len(pids) != 2 {
		t.Fatalf("pidfile lists %v, want parent and child", pids)
	}
	m.Close("helper")
	waitPidsGone(t, pids)
	if _, ok := m.Get("helper"); ok {
		t.Fatal("closed client still cached")
	}
}

// A server that starts but never answers is torn down by the group kill:
// closing its stdin cannot end it, so teardown must not wait for EOF.
func TestStdioHungServerTeardownIsBounded(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	cfg := stdioConfig(t, "-hang", "-spawn-child", "-pidfile", pidfile)
	cfg.Timeout = 300 * time.Millisecond

	start := time.Now()
	_, err := Dial(context.Background(), cfg)
	if err == nil {
		t.Fatal("dial of a hung server succeeded")
	}
	if elapsed := time.Since(start); elapsed > closeGrace+2*time.Second {
		t.Fatalf("teardown took %v, want <= %v", elapsed, closeGrace+2*time.Second)
	}
	waitPidsGone(t, readPids(t, pidfile))
}

// A child that dies during the handshake fails the dial immediately: mcp-go
// leaves the pending initialize waiting for its deadline, which the API's
// write deadline cannot afford.
func TestStdioDialFailsFastOnProcessDeath(t *testing.T) {
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := Dial(context.Background(), stdioConfig(t, "-die-fast"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("dial of an instantly-exiting server succeeded")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("dial failed after %v, want fail-fast on process death", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dial still running after 10s: process death did not fail the handshake")
	}
}

// stderr is ava's pipe and is drained: a server writing four times the OS
// pipe buffer serves calls instead of blocking on its own log.
func TestStdioStderrFloodDoesNotDeadlock(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)
	c, err := m.Connect(t.Context(), stdioConfig(t, "-stderr-flood", "262144"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if out := syncOnceAndCall(t, c, "echo", `{"text":"drained"}`); out != "drained" {
		t.Fatalf("result = %q", out)
	}
}

// The child's environment is the minimal set plus its sealed values, unless
// the operator opts into inheritance — ava's own variables, secrets master
// key included, must never leak by default.
func TestStdioEnvIsMinimalUnlessInherited(t *testing.T) {
	t.Setenv("AVA_SECRETS_MASTER_KEY", "canary-master-key")
	m := NewManager()
	t.Cleanup(m.CloseAll)

	isolated := stdioConfig(t)
	isolated.Env = map[string]string{"MCP_HELPER_PROBE": "probe-42"}
	c, err := m.Connect(t.Context(), isolated)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	out := syncOnceAndCall(t, c, "report_env", `{}`)
	if !strings.Contains(out, "MCP_HELPER_PROBE=probe-42") {
		t.Fatalf("sealed value did not reach the child: %q", out)
	}
	if strings.Contains(out, "AVA_SECRETS_MASTER_KEY") {
		t.Fatalf("ava's environment leaked into the child: %q", out)
	}
	if !strings.Contains(out, "PATH=") {
		t.Fatalf("minimal environment has no PATH: %q", out)
	}
	m.Close("helper")

	inherited := stdioConfig(t)
	inherited.ID = "inherited"
	inherited.InheritEnv = true
	inherited.Env = map[string]string{"AVA_SECRETS_MASTER_KEY": "sealed-wins"}
	c2, err := m.Connect(t.Context(), inherited)
	if err != nil {
		t.Fatalf("connect with inherit: %v", err)
	}
	out = syncOnceAndCall(t, c2, "report_env", `{}`)
	if !strings.Contains(out, "AVA_SECRETS_MASTER_KEY=sealed-wins") {
		t.Fatalf("inheritEnv did not hand over the environment: %q", out)
	}
	if strings.Contains(out, "canary-master-key") {
		t.Fatalf("a sealed value lost to the inherited spelling: %q", out)
	}
}

// A command that cannot be spawned answers with the stage error, and no
// process is left behind.
func TestStdioSpawnFailureIsReported(t *testing.T) {
	cfg := ServerConfig{ID: "missing", Transport: TransportStdio, Command: "/nonexistent/mcp-server"}
	_, err := Dial(context.Background(), cfg)
	if err == nil {
		t.Fatal("dial of a missing command succeeded")
	}
	if !errors.Is(err, exec.ErrNotFound) && !strings.Contains(err.Error(), "start") {
		t.Fatalf("error %q does not name the start stage", err)
	}
}

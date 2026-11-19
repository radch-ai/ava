//go:build unix

package mcp

import (
	"context"
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

// helperBin is the compiled testdata/mcpserver, built once per package run.
var helperBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcpserver-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temp dir:", err)
		os.Exit(1)
	}
	helperBin = filepath.Join(dir, "mcpserver")
	if out, err := exec.Command("go", "build", "-o", helperBin, "../../testdata/mcpserver").CombinedOutput(); err != nil {
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
			for _, f := range fields {
				pid, err := strconv.Atoi(f)
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
	t.Fatalf("processes %v still alive after teardown", pids)
}

func TestStdioRoundTrip(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(context.Background(), ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      []string{"-pidfile", pidfile},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "echo"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), `{"text":"hello stdio"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `"text":"hello stdio"`) {
		t.Fatalf("result = %q", out)
	}

	pids := readPids(t, pidfile)
	m.Close("helper")
	waitPidsGone(t, pids)
	if _, ok := m.Get("helper"); ok {
		t.Fatal("closed client still cached")
	}
}

func TestStdioRunTimeout(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(context.Background(), ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Timeout:   300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "sleep"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}

	start := time.Now()
	_, err = inv.InvokableRun(context.Background(), `{"ms":3000}`)
	if err == nil {
		t.Fatal("sleep under a 300ms deadline succeeded")
	}
	if !strings.Contains(err.Error(), "helper") {
		t.Fatalf("error %q does not name the server slug", err)
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("error %q is not the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("deadline fired after %v", elapsed)
	}
}

func TestStdioEnvMerge(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(context.Background(), ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Env:       map[string]string{"MCP_HELPER_PROBE": "probe-42"},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "env"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "probe-42") {
		t.Fatalf("helper env = %q, sealed value did not reach the process", out)
	}
}

// TestStdioFailedDialLeavesNoOrphan covers the dialect failure path: the
// server spawns but never answers (and has a child sharing its process
// group). Connect must fail on the handshake deadline and still kill both.
func TestStdioFailedDialLeavesNoOrphan(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	m := NewManager()
	t.Cleanup(m.CloseAll)

	_, err := m.Connect(context.Background(), ServerConfig{
		ID:        "hang",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      []string{"-hang", "-spawn-child", "-pidfile", pidfile},
		Timeout:   300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("connect to a hung server succeeded")
	}

	pids := readPids(t, pidfile)
	if len(pids) != 2 {
		t.Fatalf("pidfile lists %v, want parent and child", pids)
	}
	waitPidsGone(t, pids)
}

func TestStdioReconnectOnConfigChange(t *testing.T) {
	p1 := filepath.Join(t.TempDir(), "pid1")
	p2 := filepath.Join(t.TempDir(), "pid2")
	m := NewManager()
	t.Cleanup(m.CloseAll)
	ctx := context.Background()

	first, err := m.Connect(ctx, ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      []string{"-pidfile", p1},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	firstPids := readPids(t, p1)

	second, err := m.Connect(ctx, ServerConfig{
		ID:        "helper",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      []string{"-pidfile", p2},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if first == second {
		t.Fatal("config change reused the cached client")
	}
	waitPidsGone(t, firstPids)
	readPids(t, p2)

	tools, err := second.Tools(ctx)
	if err != nil {
		t.Fatalf("tools on new client: %v", err)
	}
	if _, err := second.Invokable(findTool(t, tools, "echo")); err != nil {
		t.Fatalf("invokable on new client: %v", err)
	}
}

// TestDialFailsFastOnProcessDeath pins the install-path failure mode: a
// stdio server that starts and exits immediately (bad command, missing
// binary dependencies, instant crash) must fail the handshake as soon as
// the process is gone -- not after the 30s default deadline, which the
// API's 15s WriteTimeout turns into an unanswerable request.
func TestDialFailsFastOnProcessDeath(t *testing.T) {
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := Dial(context.Background(), ServerConfig{
			ID:        "dead",
			Transport: TransportStdio,
			Command:   "/bin/false",
		})
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

// TestStdioStderrFloodDoesNotDeadlock pins the stderr drain: the helper
// writes four times the OS pipe buffer before serving, which blocks the
// child forever unless the client reads that pipe (mcp-go v0.43.0 never
// does — see drain.go).
func TestStdioStderrFloodDoesNotDeadlock(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.CloseAll)

	c, err := m.Connect(context.Background(), ServerConfig{
		ID:        "flood",
		Transport: TransportStdio,
		Command:   helperBin,
		Args:      []string{"-flood-stderr", "262144"},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("tools: %v", err)
	}
	inv, err := c.Invokable(findTool(t, tools, "echo"))
	if err != nil {
		t.Fatalf("invokable: %v", err)
	}
	out, err := inv.InvokableRun(context.Background(), `{"text":"drained"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "drained") {
		t.Fatalf("result = %q", out)
	}
}

package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"

	"github.com/radch-ai/ava/backend/internal/security"
)

// minimalEnv is a stdio child's default environment: enough to find the
// command and run helpers (npx and uvx need PATH; caches need HOME and
// TMPDIR), nothing of ava's own — in particular not
// AVA_SECRETS_MASTER_KEY. InheritEnv is the explicit opt-out of this
// allowlist.
var minimalEnv = []string{"PATH", "HOME", "TMPDIR"}

// stdioProc owns one spawned stdio server: the command and the single
// reaper goroutine that waits on it. mcp-go only frames JSON-RPC over the
// pipes ava hands it (transport.NewIO), so process start, lifetime and
// teardown are entirely ava's.
type stdioProc struct {
	cmd    *exec.Cmd
	stdout io.Closer     // the protocol pipe's read end, closed on teardown
	exited chan struct{} // closed by the reaper's Wait
	once   sync.Once
}

// spawnStdio starts cfg's command in its own process group and wires
// mcp-go's framing to the pipes ava owns: stdout carries the protocol,
// stdin closes for a graceful shutdown, stderr is drained (never read by
// the framing) so a chatty server cannot fill the pipe and block.
//
// The pipes are plain os.Pipe descriptors, not exec's *Pipe helpers: Wait
// then never blocks on an I/O copy and never closes a descriptor ava still
// reads. A grandchild that inherits the write end is reaped by the group
// kill, not waited for.
func spawnStdio(cfg ServerConfig, scrub *security.Scrubber) (*stdioProc, *transport.Stdio, error) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}
	pipes := []*os.File{stdinR, stdinW}
	closeAll := func() {
		for _, pipe := range pipes {
			_ = pipe.Close()
		}
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	pipes = append(pipes, stdoutR, stdoutW)
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}
	pipes = append(pipes, stderrR, stderrW)

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = childEnv(cfg)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	setPgid(cmd)
	if err := cmd.Start(); err != nil {
		closeAll()
		return nil, nil, err
	}
	// The child holds its own descriptors now: the parent's copies of the
	// child-side ends must close, or the read ends never see EOF.
	_ = stdinR.Close()
	_ = stdoutW.Close()
	_ = stderrW.Close()

	proc := &stdioProc{cmd: cmd, stdout: stdoutR, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(proc.exited)
	}()
	go drainStderr(stderrR, scrub)
	return proc, transport.NewIO(stdoutR, stdinW, nil), nil
}

// childEnv assembles the child's environment: ava's own (opt-in) or the
// minimal allowlist, then the sealed values — which win over an inherited
// spelling of the same key.
func childEnv(cfg ServerConfig) []string {
	env := make([]string, 0, len(cfg.Env)+len(minimalEnv))
	if cfg.InheritEnv {
		env = append(env, os.Environ()...)
	} else {
		for _, key := range minimalEnv {
			if value, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+value)
			}
		}
	}
	env = slices.DeleteFunc(env, func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		_, sealed := cfg.Env[key]
		return sealed
	})
	for _, key := range slices.Sorted(maps.Keys(cfg.Env)) {
		env = append(env, key+"="+cfg.Env[key])
	}
	return env
}

// maxStderrLine bounds one logged stderr line: a server that never emits a
// newline must not grow ava's memory, and it must never stop being drained.
const maxStderrLine = 8 << 10

// drainStderr reads the child's stderr to EOF through the connection's
// scrubber and writes it to the transport log. It never stops early: an
// undrained pipe fills, the child blocks on its own log write, and every
// later call dies on its deadline.
func drainStderr(pipe io.ReadCloser, scrub *security.Scrubber) {
	defer func() { _ = pipe.Close() }()
	reader := bufio.NewReaderSize(pipe, 4<<10)
	line := make([]byte, 0, maxStderrLine)
	for {
		chunk, err := reader.ReadSlice('\n')
		if room := maxStderrLine - len(line); room > 0 {
			line = append(line, chunk[:min(len(chunk), room)]...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue // the same line keeps arriving: keep draining
		}
		if text := strings.TrimSpace(string(line)); text != "" {
			text, _ = scrub.ScrubString(text)
			transportLog.Info().Str("stream", "server-stderr").Msg(text)
		}
		line = line[:0]
		if err != nil {
			return
		}
	}
}

// terminate kills the process group and joins the reaper: the single Wait
// owns process reaping, so teardown must not run its own. The group is
// signalled even when the direct child already exited — a grandchild it
// spawned keeps the group alive, and a group that is gone answers ESRCH.
// The join is bounded: SIGKILL leaves nothing to wait for except an
// unkillable process, which no deadline can fix.
func (p *stdioProc) terminate() {
	p.once.Do(func() { killGroup(p.cmd) })
	select {
	case <-p.exited:
	case <-time.After(closeGrace):
	}
	// The framing owns no descriptor of its own: the read end is ava's to
	// close once the process is gone.
	_ = p.stdout.Close()
}

// Command mcpserver is the deterministic MCP server the stdio tests drive
// over ava's real process transport: initialize, tools/list, tools/call.
//
// Flags cover the process-lifecycle edges:
//
//	-pidfile path       write "parentPid" then, with -spawn-child, "childPid"
//	-hang               start and never serve: a server that ignores stdin
//	                    EOF must still die on teardown
//	-spawn-child        start a long-lived `sleep` child in our process
//	                    group: only a group kill reaps it
//	-stderr-flood N     write N bytes to stderr before serving: an
//	                    undrained pipe fills and blocks every later call
//	-die-fast           exit before answering the handshake: a dead child
//	                    must fail the dial immediately, not on the deadline
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// longToolName overflows the 64-character model-name cap for any
// realistic slug: discovery must skip it with a reason, not rename it.
var longToolName = strings.Repeat("x", 70)

func main() {
	pidfile := flag.String("pidfile", "", "write process ids here once started")
	hang := flag.Bool("hang", false, "start but never serve")
	spawnChild := flag.Bool("spawn-child", false, "spawn a long-lived child in our process group")
	stderrFlood := flag.Int("stderr-flood", 0, "write this many bytes to stderr before serving")
	dieFast := flag.Bool("die-fast", false, "exit before answering the handshake")
	flag.Parse()

	if *dieFast {
		os.Exit(1)
	}

	pids := fmt.Sprintf("%d", os.Getpid())
	if *spawnChild {
		// The child inherits our process group: killing only the parent
		// leaves it alive, so teardown tests assert the group kill.
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "spawn child:", err)
			os.Exit(1)
		}
		pids += fmt.Sprintf(" %d", child.Process.Pid)
	}
	if *pidfile != "" {
		if err := os.WriteFile(*pidfile, []byte(pids), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "pidfile:", err)
			os.Exit(1)
		}
	}
	if *hang {
		select {} // never reads stdin; only a kill ends this
	}
	if *stderrFlood > 0 {
		chunk := bytes.Repeat([]byte{'x'}, 4096)
		for written := 0; written < *stderrFlood; written += len(chunk) {
			if _, err := os.Stderr.Write(chunk); err != nil {
				fmt.Fprintln(os.Stderr, "flood:", err)
				os.Exit(1)
			}
		}
	}

	s := server.NewMCPServer("ava-stdio-helper", "1.0.0")

	s.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("Echoes its input text."),
		mcp.WithString("text", mcp.Required(), mcp.Description("text to echo")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, _ := req.GetArguments()["text"].(string)
		return mcp.NewToolResultText(text), nil
	})

	// The environment as the child sees it: sorted KEY=VALUE lines, so a
	// test can prove what was inherited and what was sealed in.
	s.AddTool(mcp.NewTool("report_env",
		mcp.WithDescription("Returns the process environment, sorted."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		env := os.Environ()
		sort.Strings(env)
		return mcp.NewToolResultText(strings.Join(env, "\n")), nil
	})

	s.AddTool(mcp.NewTool(longToolName,
		mcp.WithDescription("Name overflows the model tool-name cap."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("long"), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

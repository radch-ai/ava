// Command mcpserver is the deterministic MCP server backend/internal/mcp
// tests drive over a real stdio transport. Tools: echo, sleep, env.
//
// Flags exist for teardown coverage:
//
//	-pidfile path   write "parentPid" then, with -spawn-child, "childPid"
//	-hang           start and never serve: proves close kills servers that
//	                ignore stdin EOF instead of blocking forever
//	-spawn-child    start a long-lived `sleep` child that shares our process
//	                group: only a group kill reaps it after the parent dies
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	pidfile := flag.String("pidfile", "", "write process ids here once started")
	hang := flag.Bool("hang", false, "start but never serve")
	spawnChild := flag.Bool("spawn-child", false, "spawn a long-lived child in our process group")
	longTool := flag.Bool("long-tool", false, "also register a tool whose name overflows the model-name cap")
	spawnLog := flag.String("spawnlog", "", "append one pid line per process start (spawn counting)")
	floodStderr := flag.Int("flood-stderr", 0, "write this many bytes to stderr before serving (pipe-drain coverage)")
	failSecondList := flag.Bool("fail-second-list", false, "exit on the second tools/list request (discovery-failure coverage)")
	flag.Parse()

	if *spawnLog != "" {
		f, err := os.OpenFile(*spawnLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "spawnlog:", err)
			os.Exit(1)
		}
		fmt.Fprintf(f, "%d\n", os.Getpid())
		_ = f.Close()
	}

	pids := fmt.Sprintf("%d", os.Getpid())
	if *spawnChild {
		// The child inherits our process group; killing only the parent
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
	if *floodStderr > 0 {
		// Blocks once the OS pipe buffer fills unless the client drains;
		// the drain in internal/mcp is what keeps this moving.
		chunk := bytes.Repeat([]byte{'x'}, 4096)
		for written := 0; written < *floodStderr; written += len(chunk) {
			if _, err := os.Stderr.Write(chunk); err != nil {
				fmt.Fprintln(os.Stderr, "flood:", err)
				os.Exit(1)
			}
		}
	}

	opts := make([]server.ServerOption, 0, 1)
	if *failSecondList {
		var lists atomic.Int32
		hooks := &server.Hooks{}
		hooks.AddBeforeListTools(func(context.Context, any, *mcp.ListToolsRequest) {
			if lists.Add(1) == 2 {
				os.Exit(1) // die before the discovery listing: install must roll back
			}
		})
		opts = append(opts, server.WithHooks(hooks))
	}
	s := server.NewMCPServer("ava-test-helper", "1.0.0", opts...)

	s.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("Echoes its input text."),
		mcp.WithString("text", mcp.Required(), mcp.Description("text to echo")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, _ := req.GetArguments()["text"].(string)
		return mcp.NewToolResultText(text), nil
	})

	s.AddTool(mcp.NewTool("sleep",
		mcp.WithDescription("Sleeps ms milliseconds, then echoes the duration."),
		mcp.WithNumber("ms", mcp.Required(), mcp.Description("milliseconds to sleep")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ms, _ := req.GetArguments()["ms"].(float64)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return mcp.NewToolResultText(fmt.Sprintf("slept %dms", int64(ms))), nil
	})

	s.AddTool(mcp.NewTool("env",
		mcp.WithDescription("Returns the MCP_HELPER_PROBE environment variable."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(os.Getenv("MCP_HELPER_PROBE")), nil
	})

	if *longTool {
		// 60 chars: "{slug}_{remote}" overflows the 64-char cap for any
		// realistic slug — the install-rejection path drives this.
		s.AddTool(mcp.NewTool(strings.Repeat("x", 60),
			mcp.WithDescription("Name overflows the model tool-name cap."),
		), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("long"), nil
		})
	}

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

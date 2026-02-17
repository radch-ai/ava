// Package node provides distributed node execution functionality for the gateway.
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/rs/zerolog"
)

var (
	ErrNodeOffline      = errors.New("node is offline")
	ErrExecutionTimeout = errors.New("execution timeout")
)

type ExecutorOptions struct {
	Registry interface {
		GetConnection(nodeID string) (*NodeConnection, bool)
	}
	Timeout time.Duration
	Logger  zerolog.Logger
}

type Executor struct {
	registry interface {
		GetConnection(nodeID string) (*NodeConnection, bool)
	}
	timeout time.Duration
	logger  zerolog.Logger
}

type pendingExecution struct {
	resultCh chan *agent.ToolResult
	errCh    chan error
}

func NewExecutor(options ExecutorOptions) *Executor {
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	return &Executor{
		registry: options.Registry,
		timeout:  options.Timeout,
		logger:   options.Logger,
	}
}

// ExecuteOnNode executes a tool on a node using Docker isolation (preferred mode).
// This is the default execution method for safe, containerized tool execution.
func (e *Executor) ExecuteOnNode(ctx context.Context, nodeID, toolName string, args map[string]any) (*agent.ToolResult, error) {
	node, exists := e.registry.GetConnection(nodeID)
	if !exists {
		return nil, ErrNodeOffline
	}

	// Strip _onhost suffix if present - caller may have used wrong function
	actualToolName := toolName
	if strings.HasSuffix(toolName, "_onhost") {
		actualToolName = strings.TrimSuffix(toolName, "_onhost")
	}

	callID := e.generateCallID()
	resultCh := make(chan *agent.ToolResult, 1)
	errCh := make(chan error, 1)

	execKey := fmt.Sprintf("%s:%s", nodeID, callID)
	registerPendingExecution(execKey, resultCh, errCh)
	defer unregisterPendingExecution(execKey)

	// Always use Docker isolation for this function
	execMsg := ExecuteMessage{
		ToolName:  actualToolName,
		Arguments: args,
		CallID:    callID,
		Isolation: "docker",
	}

	msgData, err := json.Marshal(execMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute message: %w", err)
	}

	if err := node.Conn.WriteMessage(websocket.TextMessage, msgData); err != nil {
		return nil, fmt.Errorf("failed to send message to node: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ExecuteOnNodeWithoutIsolation executes a tool directly on the host machine (bypasses Docker isolation).
// This should only be used when the user explicitly requests host execution.
func (e *Executor) ExecuteOnNodeWithoutIsolation(ctx context.Context, nodeID, toolName string, args map[string]any) (*agent.ToolResult, error) {
	node, exists := e.registry.GetConnection(nodeID)
	if !exists {
		return nil, ErrNodeOffline
	}

	// Strip _onhost suffix if present
	actualToolName := toolName
	if strings.HasSuffix(toolName, "_onhost") {
		actualToolName = strings.TrimSuffix(toolName, "_onhost")
	}

	callID := e.generateCallID()
	resultCh := make(chan *agent.ToolResult, 1)
	errCh := make(chan error, 1)

	execKey := fmt.Sprintf("%s:%s", nodeID, callID)
	registerPendingExecution(execKey, resultCh, errCh)
	defer unregisterPendingExecution(execKey)

	// Always use host isolation for this function
	execMsg := ExecuteMessage{
		ToolName:  actualToolName,
		Arguments: args,
		CallID:    callID,
		Isolation: "host",
	}

	msgData, err := json.Marshal(execMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal execute message: %w", err)
	}

	if err := node.Conn.WriteMessage(websocket.TextMessage, msgData); err != nil {
		return nil, fmt.Errorf("failed to send message to node: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *Executor) generateCallID() string {
	return uuid.New().String()
}

func (e *Executor) listenForResults(ctx context.Context, node *NodeConnection) {
	// Simply start the listening loop - the goroutine is already created by the caller
	e.listenForResultsLoop(ctx, node)
}

func (e *Executor) listenForResultsLoop(ctx context.Context, node *NodeConnection) {
	// Small delay to ensure the executor goroutine has registered pending execution
	time.Sleep(1 * time.Millisecond)

	// Continuously read messages until context is done
	for {
		// Check context before blocking read
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn := node.Conn
		if conn == nil {
			return
		}

		// ReadMessage blocks until a message arrives or error/timeout
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			// Check context again after error
			select {
			case <-ctx.Done():
				return
			default:
				// For read errors (including timeout), continue retrying
				// This handles mock timeout errors in tests
				continue
			}
		}

		if messageType != websocket.TextMessage {
			continue
		}

		// Try to parse as ToolResultMessage directly
		var resultMsg ToolResultMessage
		if err := json.Unmarshal(data, &resultMsg); err == nil && resultMsg.CallID != "" {
			execKey := fmt.Sprintf("%s:%s", node.NodeUUID, resultMsg.CallID)
			deliverResult(execKey, &resultMsg)
		}
	}
}

// Debug helper to check if we can access the connection
func (e *Executor) DebugCheckConnection(nodeID string) bool {
	conn, exists := e.registry.GetConnection(nodeID)
	if !exists {
		return false
	}
	_ = conn
	return true
}

// StartResultListener starts listening for tool execution results from a node.
// This is exported for use by external tests and should be run in a goroutine.
func (e *Executor) StartResultListener(ctx context.Context, node *NodeConnection) {
	e.listenForResults(ctx, node)
}

var (
	pendingExecutions   = make(map[string]*pendingExecution)
	pendingExecutionsMu sync.RWMutex
)

func registerPendingExecution(key string, resultCh chan *agent.ToolResult, errCh chan error) {
	pendingExecutionsMu.Lock()
	defer pendingExecutionsMu.Unlock()
	pendingExecutions[key] = &pendingExecution{
		resultCh: resultCh,
		errCh:    errCh,
	}
}

func unregisterPendingExecution(key string) {
	pendingExecutionsMu.Lock()
	defer pendingExecutionsMu.Unlock()
	delete(pendingExecutions, key)
}

func deliverResult(key string, msg *ToolResultMessage) {
	pendingExecutionsMu.RLock()
	defer pendingExecutionsMu.RUnlock()

	exec, exists := pendingExecutions[key]
	if !exists {
		return
	}

	if msg.Error != nil {
		select {
		case exec.errCh <- errors.New(*msg.Error):
		default:
		}
		return
	}

	if msg.Result != nil {
		select {
		case exec.resultCh <- msg.Result:
		default:
		}
	}
}

package agent

// Tool capture: every enabled tool rides a wrapper so the lane sees
// invocations as they happen. Eino ships callbacks and tool-call
// middleware, but its tool adapter drops the result when an invocation
// returns an error, so the wrapper is the one place where a call's
// arguments, its original result and its error are all in hand — the
// wire's call/result pair and the durable outcome row both hang off it.
//
// The turn recorder (turn_recorder.go) owns the assistant call group
// rows; this wrapper records only outcomes, pairing them by the model's
// own call id. Both writes are required: a failed write returns out of
// the invocation and fails the turn at message level.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// wrapTools rides every fetched implementation on the capture
// wrapper, once per turn — the fetch already applied permissions and
// global switches. Metadata is read here, not per invocation, and a
// selected tool that cannot describe itself fails the turn: a tool
// the model was promised must never vanish silently.
func (a *UserAgent) wrapTools(ctx context.Context, rec *turnRecorder, impls []tool.InvokableTool) ([]tool.BaseTool, error) {
	if len(impls) == 0 {
		return nil, nil
	}
	wrapped := make([]tool.BaseTool, 0, len(impls))
	for _, impl := range impls {
		info, err := impl.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool metadata: %w", err)
		}
		if info == nil || info.Name == "" {
			return nil, errors.New("tool metadata: missing name")
		}
		wrapped = append(wrapped, &capturingTool{inner: impl, info: info, rec: rec})
	}
	return wrapped, nil
}

// capturingTool delegates to the wrapped implementation and reports
// each invocation through the turn's recorder: the call before it runs,
// the outcome after. info is the turn's snapshot, so the runner's
// schema and the wire's name agree without re-reading the
// implementation. Reports go out on the recorder's emit context, which
// outlives the turn's cancel — a failed or stopped turn still reports
// its last tool's outcome.
type capturingTool struct {
	inner tool.InvokableTool
	info  *schema.ToolInfo
	rec   *turnRecorder
}

func (t *capturingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *capturingTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	// The model's own call id is metadata: repeated tool rounds may reuse
	// it, so the invocation gets a server-assigned identity of its own.
	// A missing call id is still a malformed response.
	callID := compose.GetToolCallID(ctx)
	if callID == "" {
		return "", errors.New("tool call has no id")
	}
	invocationID := uuid.NewString()
	// Recorded intent precedes the external effect: a journal that cannot
	// hold it stops the invocation before the tool runs.
	if err := t.rec.recordStart(invocationID, callID, t.info.Name, args); err != nil {
		return "", err
	}
	result, err := t.inner.InvokableRun(ctx, args, opts...)
	invocationErr := err
	if err != nil {
		// Named on the way out so the turn's error event can say
		// which tool failed without the runner's internal framing.
		err = &toolError{name: t.info.Name, err: err}
	}
	// The required outcome write wins over the invocation's own error:
	// the transcript must not lose the row. The returned error is not
	// tool-attributed, so the turn fails at message level.
	if perr := t.rec.recordOutcome(invocationID, callID, t.info.Name, result, invocationErr); perr != nil {
		return result, perr
	}
	return result, err
}

// toolError carries the name of a tool invocation failed in.
type toolError struct {
	name string
	err  error
}

func (e *toolError) Error() string { return e.err.Error() }
func (e *toolError) Unwrap() error { return e.err }

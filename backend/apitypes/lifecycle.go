package apitypes

// Conversation lifecycle events: the durable, ordered facts every
// subscribed device receives. The journal stores a LifecycleEvent
// verbatim and replay serves the same bytes; live delivery carries the
// same shape. Fields are set per Kind — unset ones stay omitted.

// Lifecycle event kinds.
const (
	// LifecycleItemAccepted reports an admitted submission under its
	// stable item id, before any execution state exists.
	LifecycleItemAccepted = "item_accepted"
	// LifecycleItemStarted reports execution claiming the item and
	// resolving its thread; a prompt's user row commits with it.
	LifecycleItemStarted = "item_started"
	// LifecycleMessageCommitted reports a nonterminal assistant row
	// committed mid-turn, carrying its canonical row identity.
	LifecycleMessageCommitted = "message_committed"
	// LifecycleToolStarted reports one invocation authorized to begin.
	LifecycleToolStarted = "tool_started"
	// LifecycleToolFinished reports that invocation's actual result or
	// error row committed.
	LifecycleToolFinished = "tool_finished"
	// LifecycleItemFinished reports the item's one terminal outcome and
	// any final rows committed with it.
	LifecycleItemFinished = "item_finished"
	// LifecycleThreadChanged reports a command's new thread, its
	// predecessor, and its divider.
	LifecycleThreadChanged = "thread_changed"
)

// Terminal item outcomes.
const (
	ItemCompleted   = "completed"
	ItemFailed      = "failed"
	ItemStopped     = "stopped"
	ItemCancelled   = "cancelled"
	ItemInterrupted = "interrupted"
)

// LifecycleEvent is one committed lifecycle fact. Pos is the agent's
// journal position (monotonic, never reused) and BatchEnd marks the last
// event of its committing transaction — the only place replay pages and
// retention cuts may end.
type LifecycleEvent struct {
	Pos      int64  `json:"pos"`
	BatchEnd bool   `json:"batchEnd"`
	Kind     string `json:"kind"`
	ItemID   string `json:"itemId"`
	// ItemKind classifies the submission: a prompt runs a model turn, a
	// command switches threads.
	ItemKind string `json:"itemKind,omitempty"`
	// Content is the submission text, carried by item_accepted and by
	// the recovery notice of an item that never started.
	Content string `json:"content,omitempty"`
	// Model is the selection captured at admission; nil is default
	// routing.
	Model *ModelEntry `json:"model,omitempty"`
	// ThreadID is the execution thread the fact committed in.
	ThreadID string `json:"threadId,omitempty"`
	// PreviousThreadID names the thread a switch replaced.
	PreviousThreadID string `json:"previousThreadId,omitempty"`
	// UserMessageID names the stored user row a prompt's start
	// committed — absent for commands.
	UserMessageID string `json:"userMessageId,omitempty"`
	// Divider is the display record of a thread boundary.
	Divider *ThreadMessage `json:"divider,omitempty"`
	// Rows are the canonical display rows this event committed, in
	// display order.
	Rows []ThreadMessage `json:"rows,omitempty"`
	// InvocationID is the server-assigned identity of one tool
	// invocation; CallID is the provider's own id, metadata only.
	InvocationID string  `json:"invocationId,omitempty"`
	CallID       string  `json:"callId,omitempty"`
	ToolName     string  `json:"toolName,omitempty"`
	ToolArgs     string  `json:"toolArgs,omitempty"`
	ToolResult   string  `json:"toolResult,omitempty"`
	ToolError    *string `json:"toolError,omitempty"`
	// Outcome is a terminal item's outcome; Reason explains it.
	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// CreatedAt is the fact's commit time, ISO-formatted like every
	// other display timestamp.
	CreatedAt string `json:"createdAt,omitempty"`
}

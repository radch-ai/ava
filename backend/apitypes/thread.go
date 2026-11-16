package apitypes

import "encoding/json"

// ThreadRole is the kind of row a thread message is.
type ThreadRole string

const (
	ThreadRoleUser      ThreadRole = "user"
	ThreadRoleAssistant ThreadRole = "assistant"
	ThreadRoleTool      ThreadRole = "tool"
	ThreadRoleError     ThreadRole = "error"
)

// ToolEnvelope is the content of a role='tool' row: one completed
// invocation, call and result in one record — the memory gate can
// split it into tool-call/tool-result messages without re-storing.
// Args and Result stay raw JSON; their shape is the tool's, not the
// thread's.
type ToolEnvelope struct {
	Name   string          `json:"name"`
	Args   json.RawMessage `json:"args"`
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error,omitempty"`
}

// ThreadMessage is one message in the agent's conversation.
type ThreadMessage struct {
	ID        string     `json:"id"`
	Role      ThreadRole `json:"role"`
	Content   string     `json:"content"`
	CreatedAt string     `json:"createdAt"`
}

// ThreadMessageDraft is a message queued for the thread. An empty ID
// has the sink mint one; a supplied ID (terminal error rows, so the
// failure's event can name its stored row) is used verbatim. The
// timestamp stays the sink's — it owns ordering.
type ThreadMessageDraft struct {
	ID      string     `json:"id,omitempty"`
	Role    ThreadRole `json:"role"`
	Content string     `json:"content"`
}

// ThreadPage is one page of the thread, its items ordered
// oldest→newest — the display order. NextCursor names the page's
// oldest item and is null on the last page; the page it names is
// strictly older.
type ThreadPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

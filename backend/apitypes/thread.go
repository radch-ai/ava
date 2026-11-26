package apitypes

// ThreadRole is the kind of row a thread message is.
type ThreadRole string

const (
	ThreadRoleUser      ThreadRole = "user"
	ThreadRoleAssistant ThreadRole = "assistant"
	ThreadRoleTool      ThreadRole = "tool"
	ThreadRoleError     ThreadRole = "error"
	// ThreadRoleDivider is display-only: generated from thread
	// records, never stored.
	ThreadRoleDivider ThreadRole = "divider"
)

// ThreadMessage is one message in the agent's conversation. A
// divider's ID is its thread id — a different namespace from message
// ids, so display keys never collide.
type ThreadMessage struct {
	ID        string     `json:"id"`
	Role      ThreadRole `json:"role"`
	Content   string     `json:"content"`
	CreatedAt string     `json:"createdAt"`
	// IsSummary marks a compaction successor's seeded summary row;
	// clients collapse it behind its boundary instead of rendering
	// the summary as prose.
	IsSummary bool `json:"isSummary,omitempty"`
}

// ThreadPage is one page of one thread, its items ordered
// oldest→newest — the display order. NextCursor is opaque to
// clients and is null on the last page: the first thread of the
// chain, whose page it never names. ThreadID names the thread these
// items belong to.
type ThreadPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor *string         `json:"nextCursor"`
	ThreadID   string          `json:"threadId"`
}

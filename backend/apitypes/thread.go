package apitypes

// ThreadRole is the kind of row a thread message is.
type ThreadRole string

const (
	ThreadRoleUser      ThreadRole = "user"
	ThreadRoleAssistant ThreadRole = "assistant"
	ThreadRoleTool      ThreadRole = "tool"
	ThreadRoleError     ThreadRole = "error"
)

// ThreadMessage is one message in the agent's conversation.
type ThreadMessage struct {
	ID        string     `json:"id"`
	Role      ThreadRole `json:"role"`
	Content   string     `json:"content"`
	CreatedAt string     `json:"createdAt"`
}

// ThreadPage is one page of the thread, its items ordered
// oldest→newest — the display order. NextCursor names the page's
// oldest item and is null on the last page; the page it names is
// strictly older.
type ThreadPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

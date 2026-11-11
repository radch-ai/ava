package apitypes

// ThreadRole is who said a thread message.
type ThreadRole string

const (
	ThreadRoleUser      ThreadRole = "user"
	ThreadRoleAssistant ThreadRole = "assistant"
)

// ThreadMessage is one message in the agent's conversation.
type ThreadMessage struct {
	ID        string     `json:"id"`
	Role      ThreadRole `json:"role"`
	Content   string     `json:"content"`
	CreatedAt string     `json:"createdAt"`
}

// ThreadPage is one newest-first page of the thread. NextCursor names
// the page's oldest item and is null on the last page.
type ThreadPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

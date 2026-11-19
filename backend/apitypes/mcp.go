package apitypes

// MCP server wire types. Secret VALUES never appear in any response:
// requests accept env/headers maps, responses list key names only.

// McpServer is one configured MCP server as the API returns it:
// connection config plus diagnostics. EnvKeys/HeaderKeys are the
// sealed maps' key names, never their values.
type McpServer struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Transport   string   `json:"transport"` // "stdio" | "http"
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
	EnvKeys     []string `json:"envKeys,omitempty"`
	HeaderKeys  []string `json:"headerKeys,omitempty"`
	TimeoutMs   *int64   `json:"timeoutMs,omitempty"`
	Enabled     bool     `json:"enabled"`
	ToolCount   int64    `json:"toolCount"`
	LastError   *string  `json:"lastError,omitempty"`
	RefreshedAt *int64   `json:"refreshedAt,omitempty"`
}

// McpDiscoveredTool is one tool a server reported, in create/refresh
// responses: the catalog id plus the metadata that was upserted.
type McpDiscoveredTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateMcpServerRequest is POST /api/mcp/servers' body. Env and
// Headers are sealed on save and never echoed back.
type CreateMcpServerRequest struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMs *int64            `json:"timeoutMs,omitempty"`
}

// UpdateMcpServerRequest is PATCH /api/mcp/servers/{id}'s body with
// absent-keeps semantics. Nil maps and slices keep the stored values;
// a present map, slice, or pointer replaces what is stored
// wholesale — an explicit empty object clears the map. ClearTimeoutMs drops
// a stored timeout (pointer fields cannot express absent-vs-null).
type UpdateMcpServerRequest struct {
	Name           *string           `json:"name,omitempty"`
	Transport      *string           `json:"transport,omitempty"`
	Command        *string           `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	URL            *string           `json:"url,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	TimeoutMs      *int64            `json:"timeoutMs,omitempty"`
	ClearTimeoutMs bool              `json:"clearTimeoutMs,omitempty"`
	// Enabled is the global server switch -- a pure state flip: no
	// verify, no config write. Disabling closes the live client;
	// enabling reconnects best-effort (degrade, never fail).
	Enabled *bool `json:"enabled,omitempty"`
}

// McpServerWithToolsResponse is the body of every write that answers a
// server plus its listing: create, patch, and refresh.
type McpServerWithToolsResponse struct {
	Server McpServer           `json:"server"`
	Tools  []McpDiscoveredTool `json:"tools"`
}

// GetMcpServersResponse is GET /api/mcp/servers' body.
type GetMcpServersResponse struct {
	Servers []McpServer `json:"servers"`
}

// DeleteMcpServerResponse is DELETE /api/mcp/servers/{id}'s body.
type DeleteMcpServerResponse struct {
	Message string `json:"message"`
}

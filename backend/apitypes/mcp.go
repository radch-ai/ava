package apitypes

// MCP server wire types. Secret VALUES never appear in any response:
// requests accept headers and env maps, responses list key names only.
// Two transports: "http" (streamable HTTP) and "stdio" (a local process
// ava spawns).

// McpServer is one configured MCP server as the API returns it:
// connection config plus diagnostics. HeaderKeys and EnvKeys are the
// sealed maps' key names, never their values. CatalogState is "active"
// or "paused" (a security-rejected discovery generation pauses new
// calls). Transport is "http" or "stdio": only the fields of its own
// transport are populated.
type McpServer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Transport    string   `json:"transport"` // "http" | "stdio"
	URL          string   `json:"url,omitempty"`
	Command      string   `json:"command,omitempty"`
	Args         []string `json:"args,omitempty"`
	HeaderKeys   []string `json:"headerKeys,omitempty"`
	EnvKeys      []string `json:"envKeys,omitempty"`
	InheritEnv   bool     `json:"inheritEnv,omitempty"`
	TimeoutMs    *int64   `json:"timeoutMs,omitempty"`
	Enabled      bool     `json:"enabled"`
	ToolCount    int64    `json:"toolCount"`
	CatalogState string   `json:"catalogState"` // "active" | "paused"
	// AllowPrivateNetwork approves loopback/LAN destinations for this
	// endpoint; AllowPlaintext approves plaintext HTTP there,
	// credentials included.
	AllowPrivateNetwork bool    `json:"allowPrivateNetwork"`
	AllowPlaintext      bool    `json:"allowPlaintext"`
	LastError           *string `json:"lastError,omitempty"`
}

// McpDiscoveredTool is one tool a server reported, in create/refresh
// responses: the catalog id plus the metadata that was published.
type McpDiscoveredTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateMcpServerRequest is POST /api/mcp/servers' body. Headers and env
// are sealed on save and never echoed back. The two policy flags are
// absent means false: a fresh server has no destination or plaintext
// exception. Transport defaults to "http"; a stdio server requires
// Command and AcknowledgeProcessExecution, and rejects the http-only
// fields.
type CreateMcpServerRequest struct {
	Name                string            `json:"name"`
	Transport           string            `json:"transport,omitempty"` // "http" (default) | "stdio"
	URL                 string            `json:"url,omitempty"`
	Command             string            `json:"command,omitempty"`
	Args                []string          `json:"args,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	InheritEnv          bool              `json:"inheritEnv,omitempty"`
	TimeoutMs           *int64            `json:"timeoutMs,omitempty"`
	AllowPrivateNetwork *bool             `json:"allowPrivateNetwork,omitempty"`
	AllowPlaintext      *bool             `json:"allowPlaintext,omitempty"`
	// AcknowledgeProcessExecution accepts that a stdio server runs a
	// process with the administrator's privileges.
	AcknowledgeProcessExecution bool `json:"acknowledgeProcessExecution,omitempty"`
}

// UpdateMcpServerRequest is PATCH /api/mcp/servers/{id}'s body with
// absent-keeps semantics. Nil maps and slices keep the stored values; a
// present map, slice, or pointer replaces what is stored wholesale —
// an explicit empty object clears the map. ClearTimeoutMs drops a stored
// timeout (pointer fields cannot express absent-vs-null). A URL change
// resets both policy flags unless the same patch re-approves them
// (a destination change invalidates its approval). A transport change is
// a connection edit; a changed command or args clears the process
// acknowledgment unless the same patch acknowledges again.
type UpdateMcpServerRequest struct {
	Name           *string           `json:"name,omitempty"`
	Transport      *string           `json:"transport,omitempty"`
	URL            *string           `json:"url,omitempty"`
	Command        *string           `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	InheritEnv     *bool             `json:"inheritEnv,omitempty"`
	TimeoutMs      *int64            `json:"timeoutMs,omitempty"`
	ClearTimeoutMs bool              `json:"clearTimeoutMs,omitempty"`
	// AllowPrivateNetwork/AllowPlaintext: absent keeps, explicit false
	// clears, true approves.
	AllowPrivateNetwork *bool `json:"allowPrivateNetwork,omitempty"`
	AllowPlaintext      *bool `json:"allowPlaintext,omitempty"`
	// AcknowledgeProcessExecution is required when this patch changes a
	// stdio server's command or args.
	AcknowledgeProcessExecution bool `json:"acknowledgeProcessExecution,omitempty"`
	// Enabled is the global server switch -- a pure state flip: no
	// verify, no config write. Disabling closes the live client and
	// cancels its admitted calls; enabling reconnects best-effort
	// (degrade, never fail).
	Enabled *bool `json:"enabled,omitempty"`
}

// McpSkippedTool is one listed tool a discovery pass left out for a
// cosmetic reason — a name the provider wire contract cannot carry.
// It is reported, never renamed or silently dropped.
type McpSkippedTool struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// McpServerWithToolsResponse is the body of every write that answers a
// server plus its listing: create, patch, and refresh. SkippedTools names
// the entries a discovery pass could not represent.
type McpServerWithToolsResponse struct {
	Server       McpServer           `json:"server"`
	Tools        []McpDiscoveredTool `json:"tools"`
	SkippedTools []McpSkippedTool    `json:"skippedTools,omitempty"`
}

// GetMcpServersResponse is GET /api/mcp/servers' body.
type GetMcpServersResponse struct {
	Servers []McpServer `json:"servers"`
}

// DeleteMcpServerResponse is DELETE /api/mcp/servers/{id}'s body.
type DeleteMcpServerResponse struct {
	Message string `json:"message"`
}

// McpToolDefinitionResponse is GET /api/mcp/servers/{id}/tools/{toolId}/
// definition's body: the stored, sanitized definition the settings page
// inspects, plus the authority behind it: the endpoint (http) or the
// command (stdio), and whether the connection carries credentials or
// rides plaintext.
type McpToolDefinitionResponse struct {
	DefinitionJSON string `json:"definitionJson"`
	// Endpoint is the server's stored URL with userinfo masked; stdio
	// rows report Command instead.
	Endpoint string `json:"endpoint,omitempty"`
	Command  string `json:"command,omitempty"`
	// HasCredentials reports a non-empty secrets key set (headers or
	// env); AllowPlaintext reports the stored plaintext exception,
	// AllowPrivateNetwork the private-network exception (both are
	// http-only facts; stdio rows report false).
	HasCredentials      bool `json:"hasCredentials"`
	AllowPlaintext      bool `json:"allowPlaintext"`
	AllowPrivateNetwork bool `json:"allowPrivateNetwork"`
}

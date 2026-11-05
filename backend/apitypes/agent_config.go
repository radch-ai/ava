package apitypes

// AgentEditableFields is every field the config endpoints carry for
// editing: display identity (name, icon) plus the prompt texts and
// the tool list. Pointers mark what a PATCH carries — absent fields
// keep their stored value — and this same type is the PATCH body.
type AgentEditableFields struct {
	Name                  *string     `json:"name,omitempty"`
	Icon                  *string     `json:"icon,omitempty"`
	Soul                  *SoulConfig `json:"soul,omitempty"`
	UserContext           *string     `json:"userContext,omitempty"`
	OperatingInstructions *string     `json:"operatingInstructions,omitempty"`
	ToolsContext          *string     `json:"toolsContext,omitempty"`
	ToolsEnabled          *[]string   `json:"toolsEnabled,omitempty"`
}

// AgentConfig is the editable fields plus the heartbeat. In the
// detail body the editable fields are all carried; name/icon ride on
// the identity instead and stay nil here. Cron jobs are fetched
// lazily via /api/agent/{id}/cron-jobs.
type AgentConfig struct {
	AgentEditableFields
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

// AgentConfigWithIdentity is the GET/PATCH /api/agent/{id}/config
// body: identity plus the whole AgentConfig at one level.
type AgentConfigWithIdentity struct {
	AgentIdentity
	AgentConfig
}

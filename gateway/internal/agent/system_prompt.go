package agent

import (
	"strings"

	"github.com/radch-ai/ava/backend/apitypes"
)

// BuildSystemPrompt renders an agent's config as a turn's system
// prompt: the soul under one header (personality before identity —
// the type is one unit), operating instructions, user information,
// and tool guidance, each under its header. Empty sections are
// skipped; an empty config builds an empty prompt.
func BuildSystemPrompt(cfg *apitypes.AgentConfig) string {
	if cfg == nil {
		return ""
	}
	var sections []string
	if cfg.Soul != nil {
		var parts []string
		for _, part := range []string{cfg.Soul.Personality, cfg.Soul.Identity} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			sections = append(sections, "## Soul\n\n"+strings.Join(parts, "\n\n"))
		}
	}
	if v := ptrText(cfg.OperatingInstructions); v != "" {
		sections = append(sections, "## Operating Instructions\n\n"+v)
	}
	if v := ptrText(cfg.UserContext); v != "" {
		sections = append(sections, "## User Information\n\n"+v)
	}
	if v := ptrText(cfg.ToolsContext); v != "" {
		sections = append(sections, "## Tool Guidance\n\n"+v)
	}
	return strings.Join(sections, "\n\n")
}

// ptrText reads a patch-shaped pointer; nil reads as empty.
func ptrText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

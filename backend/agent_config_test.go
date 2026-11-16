package backend_test

// GetAgentConfig pins the lane's config read: the stored row maps to
// the editable fields the prompt builder consumes, and a missing
// agent surfaces sql.ErrNoRows.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestGetAgentConfigMapsPromptFields(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	if _, err := sqlDB.ExecContext(context.Background(),
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, user_context,
			operating_instructions, tools_context, tools_enabled)
		 VALUES (?, 'Persona', 'globe', 'warm', 'companion', NULL, 'house rules', 'prefer search', '["search"]')`,
		"persona-1"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	cfg, err := svc.GetAgentConfig(context.Background(), "persona-1")
	if err != nil {
		t.Fatalf("GetAgentConfig: %v", err)
	}
	if cfg.Soul == nil || cfg.Soul.Personality != "warm" || cfg.Soul.Identity != "companion" {
		t.Errorf("soul = %+v, want personality/identity", cfg.Soul)
	}
	if cfg.OperatingInstructions == nil || *cfg.OperatingInstructions != "house rules" {
		t.Errorf("operating instructions = %v, want %q", cfg.OperatingInstructions, "house rules")
	}
	if cfg.UserContext != nil {
		t.Errorf("user context = %q, want nil for NULL column", *cfg.UserContext)
	}
	if cfg.ToolsContext == nil || *cfg.ToolsContext != "prefer search" {
		t.Errorf("tools context = %v, want %q", cfg.ToolsContext, "prefer search")
	}
	if cfg.ToolsEnabled == nil || len(*cfg.ToolsEnabled) != 1 || (*cfg.ToolsEnabled)[0] != "search" {
		t.Errorf("tools enabled = %v, want [search]", cfg.ToolsEnabled)
	}
}

func TestGetAgentConfigUnknownAgentFailsNoRows(t *testing.T) {
	_, svc := resolverDB(t)

	_, err := svc.GetAgentConfig(context.Background(), "ghost")
	if err == nil {
		t.Fatal("GetAgentConfig on unknown agent: no error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("error = %v, want sql.ErrNoRows (wrapped)", err)
	}
}

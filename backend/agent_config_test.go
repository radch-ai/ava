package backend_test

// GetAgentConfig pins the lane's config read: the stored row maps to
// the editable fields the prompt builder consumes, and a missing
// agent surfaces sql.ErrNoRows.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

func TestGetAgentConfigMapsPromptFields(t *testing.T) {
	sqlDB, svc := resolverDB(t)
	testdb.SeedAgent(t, sqlDB, db.CreateAgentParams{
		ID:                    "persona-1",
		Name:                  "Persona",
		Icon:                  "globe",
		SoulPersonality:       "warm",
		SoulIdentity:          "companion",
		OperatingInstructions: "house rules",
		ToolsContext:          "prefer search",
	})

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

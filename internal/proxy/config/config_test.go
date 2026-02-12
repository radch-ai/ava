// Package config provides proxy configuration loading.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.yaml")

	// Create a simple config
	configContent := `
models:
  gpt-4o-mini:
    provider: openai
    input_cost_per_million: 0.15
    output_cost_per_million: 0.60
    max_tokens: 128000
    tier: economy

router:
  model: gpt-4o-mini
  timeout: 5s
  max_retries: 2
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}

	// Check router config
	if cfg.Router.Model != "gpt-4o-mini" {
		t.Errorf("Expected router model gpt-4o-mini, got %s", cfg.Router.Model)
	}
}

func TestGetPlan(t *testing.T) {
	plan := GetPlan("basic")
	if plan == nil {
		t.Fatal("GetPlan returned nil for basic")
	}

	if plan.Name != "basic" {
		t.Errorf("Expected plan name 'basic', got %s", plan.Name)
	}
}

func TestGetAllPlans(t *testing.T) {
	plans := GetAllPlans()
	if len(plans) == 0 {
		t.Error("GetAllPlans returned empty list")
	}

	// Check that expected plans exist
	expectedPlans := []string{"basic", "pro", "enterprise"}
	for _, name := range expectedPlans {
		found := false
		for _, plan := range plans {
			if plan.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Plan %s not found", name)
		}
	}
}

func TestPlanCredits(t *testing.T) {
	plan := GetPlan("pro")
	if plan == nil {
		t.Fatal("GetPlan returned nil for pro")
	}

	if plan.MonthlyLLMCredits <= 0 {
		t.Errorf("Expected positive credits for pro plan, got %f", plan.MonthlyLLMCredits)
	}
}

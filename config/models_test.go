// Package config provides configuration loading for the gateway.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelsConfig(t *testing.T) {
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

	cfg, err := LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("LoadModelsConfig returned nil")
	}

	// Check router config
	if cfg.Router.Model != "gpt-4o-mini" {
		t.Errorf("Expected router model gpt-4o-mini, got %s", cfg.Router.Model)
	}
}

func TestGetModel(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.yaml")

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

	cfg, err := LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	model, ok := cfg.GetModel("gpt-4o-mini")
	if !ok {
		t.Fatal("GetModel returned false for existing model")
	}

	if model.Provider != "openai" {
		t.Errorf("Expected provider openai, got %s", model.Provider)
	}
}

func TestGetAllModels(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.yaml")

	configContent := `
models:
  gpt-4o-mini:
    provider: openai
    input_cost_per_million: 0.15
    output_cost_per_million: 0.60
    max_tokens: 128000
    tier: economy
  claude-3-haiku:
    provider: anthropic
    input_cost_per_million: 0.25
    output_cost_per_million: 1.25
    max_tokens: 200000
    tier: economy

router:
  model: gpt-4o-mini
  timeout: 5s
  max_retries: 2
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	models := cfg.GetAllModels()
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}
}

func TestGetModelsByProvider(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.yaml")

	configContent := `
models:
  gpt-4o-mini:
    provider: openai
    input_cost_per_million: 0.15
    output_cost_per_million: 0.60
    max_tokens: 128000
    tier: economy
  gpt-4o:
    provider: openai
    input_cost_per_million: 2.50
    output_cost_per_million: 10.00
    max_tokens: 128000
    tier: premium
  claude-3-haiku:
    provider: anthropic
    input_cost_per_million: 0.25
    output_cost_per_million: 1.25
    max_tokens: 200000
    tier: economy

router:
  model: gpt-4o-mini
  timeout: 5s
  max_retries: 2
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	openaiModels := cfg.GetModelsByProvider("openai")
	if len(openaiModels) != 2 {
		t.Errorf("Expected 2 openai models, got %d", len(openaiModels))
	}

	anthropicModels := cfg.GetModelsByProvider("anthropic")
	if len(anthropicModels) != 1 {
		t.Errorf("Expected 1 anthropic model, got %d", len(anthropicModels))
	}
}

func TestGetModelsByTier(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.yaml")

	configContent := `
models:
  gpt-4o-mini:
    provider: openai
    input_cost_per_million: 0.15
    output_cost_per_million: 0.60
    max_tokens: 128000
    tier: economy
  gpt-4o:
    provider: openai
    input_cost_per_million: 2.50
    output_cost_per_million: 10.00
    max_tokens: 128000
    tier: premium

router:
  model: gpt-4o-mini
  timeout: 5s
  max_retries: 2
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	economyModels := cfg.GetModelsByTier("economy")
	if len(economyModels) != 1 {
		t.Errorf("Expected 1 economy model, got %d", len(economyModels))
	}

	premiumModels := cfg.GetModelsByTier("premium")
	if len(premiumModels) != 1 {
		t.Errorf("Expected 1 premium model, got %d", len(premiumModels))
	}
}

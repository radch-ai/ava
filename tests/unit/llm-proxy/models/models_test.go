package models_test

import (
	"os"
	"path/filepath"
	"testing"

	llm_proxy_models "github.com/nevaria-ai/gateway/internal/llm-proxy/models"
)

func TestModelRegistryInitialization(t *testing.T) {
	// Create a new registry
	registry := llm_proxy_models.NewRegistry()
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}

	// Find the project root and config file
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Navigate to the project root
	projectRoot := filepath.Join(wd, "..", "..", "..")
	configPath := filepath.Join(projectRoot, "config", "models.yaml")

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skipf("Config file not found: %s", configPath)
	}

	// Test loading models from YAML configuration
	err = registry.LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("failed to load registry: %v", err)
	}

	// Verify router model is configured
	routerModelName := registry.GetRouterModel()
	if routerModelName == "" {
		t.Error("expected router model to be configured")
	} else if routerModelName != "gpt-4o-mini" {
		t.Errorf("expected router model name 'gpt-4o-mini', got %s", routerModelName)
	}

	// Verify at least some models are loaded
	allModels := registry.GetAllModels()
	if len(allModels) == 0 {
		t.Error("expected at least one model in registry")
	}

	// Verify we can get specific models
	if model, exists := registry.GetModel("gpt-4o"); !exists {
		t.Error("expected gpt-4o model to exist")
	} else {
		if model.Provider != "openai" {
			t.Errorf("expected gpt-4o provider 'openai', got %s", model.Provider)
		}
	}
}

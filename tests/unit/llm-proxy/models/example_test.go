package models_test

import (
	"os"
	"path/filepath"
	"testing"

	llm_proxy_models "github.com/nevaria-ai/gateway/internal/llm-proxy/models"
)

// TestLoadRealModelsConfig tests loading the actual models.yaml file
func TestLoadRealModelsConfig(t *testing.T) {
	// Get the project root directory
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

	// Load the configuration
	registry := llm_proxy_models.NewRegistry()
	err = registry.LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("Failed to load models config: %v", err)
	}

	// Verify some models exist
	testModels := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"o1-preview",
		"claude-3-5-sonnet",
		"claude-3-5-haiku",
		"claude-3-opus",
		"gemini-2.0-flash",
		"gemini-2.5-pro",
	}

	for _, modelName := range testModels {
		cfg, exists := registry.GetModel(modelName)
		if !exists {
			t.Errorf("Model %s not found in registry", modelName)
		} else {
			t.Logf("Model %s: provider=%s, tier=%s, input_cost=$%.2f/M, output_cost=$%.2f/M",
				modelName, cfg.Provider, cfg.Tier,
				cfg.InputCostPerMillion, cfg.OutputCostPerMillion)
		}
	}

	// Verify router model is set
	routerModel := registry.GetRouterModel()
	if routerModel == "" {
		t.Error("Router model not set")
	} else {
		t.Logf("Router model: %s", routerModel)
	}

	// Verify all OpenAI models
	openaiModels := registry.GetModelsByProvider("openai")
	t.Logf("OpenAI models: %d", len(openaiModels))
	for _, m := range openaiModels {
		t.Logf("  - %s (%s)", m.Name, m.Tier)
	}

	// Verify all Anthropic models
	anthropicModels := registry.GetModelsByProvider("anthropic")
	t.Logf("Anthropic models: %d", len(anthropicModels))
	for _, m := range anthropicModels {
		t.Logf("  - %s (%s)", m.Name, m.Tier)
	}

	// Verify all Google models
	googleModels := registry.GetModelsByProvider("google")
	t.Logf("Google models: %d", len(googleModels))
	for _, m := range googleModels {
		t.Logf("  - %s (%s)", m.Name, m.Tier)
	}

	// Verify tier counts
	economyModels := registry.GetModelsByTier(llm_proxy_models.TierEconomy)
	premiumModels := registry.GetModelsByTier(llm_proxy_models.TierPremium)
	ultraModels := registry.GetModelsByTier(llm_proxy_models.TierUltra)

	t.Logf("Economy models: %d", len(economyModels))
	t.Logf("Premium models: %d", len(premiumModels))
	t.Logf("Ultra models: %d", len(ultraModels))

	// Verify total count
	allModels := registry.GetAllModels()
	t.Logf("Total models: %d", len(allModels))
}

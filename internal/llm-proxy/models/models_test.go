package models

import (
	"os"
	"testing"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
)

func TestModelConfig(t *testing.T) {
	cfg := ModelConfig{
		Name:                 "gpt-4o-mini",
		Provider:             "openai",
		InputCostPerMillion:  0.15,
		OutputCostPerMillion: 0.60,
		MaxTokens:            128000,
		Tier:                 TierEconomy,
	}

	if cfg.Tier != TierEconomy {
		t.Errorf("Expected economy tier, got %s", cfg.Tier)
	}

	if cfg.Name != "gpt-4o-mini" {
		t.Errorf("Expected gpt-4o-mini, got %s", cfg.Name)
	}
}

func TestModelTierConstants(t *testing.T) {
	if TierEconomy != "economy" {
		t.Error("TierEconomy constant incorrect")
	}

	if TierPremium != "premium" {
		t.Error("TierPremium constant incorrect")
	}

	if TierUltra != "ultra" {
		t.Error("TierUltra constant incorrect")
	}
}

func TestCalculateCost(t *testing.T) {
	cfg := ModelConfig{
		InputCostPerMillion:  2.50,
		OutputCostPerMillion: 10.00,
	}

	cost := CalculateCost(&cfg, 1000, 500)
	expected := 0.0075 // (1*2.50 + 0.5*10.00) / 1000000 = 0.0000025 + 0.000005 = 0.0000075

	if cost != expected {
		t.Errorf("Expected %f, got %f", expected, cost)
	}
}

func TestCalculateCostLarge(t *testing.T) {
	cfg := ModelConfig{
		InputCostPerMillion:  3.00,
		OutputCostPerMillion: 15.00,
	}

	// Test with larger token counts
	cost := CalculateCost(&cfg, 100000, 50000)
	expected := 1.05 // (100000*3.00 + 50000*15.00) / 1000000 = (300000 + 750000) / 1000000 = 1.05

	if cost != expected {
		t.Errorf("Expected %f, got %f", expected, cost)
	}
}

func TestEstimateCost(t *testing.T) {
	cfg := ModelConfig{
		InputCostPerMillion:  2.50,
		OutputCostPerMillion: 10.00,
	}

	estimate := &proxy.TokenEstimate{
		Input:  1000,
		Output: 500,
	}

	costEstimate := EstimateCost(&cfg, estimate)

	expectedInput := 0.0025  // 1000 * 2.50 / 1000000
	expectedOutput := 0.0050 // 500 * 10.00 / 1000000
	expectedTotal := 0.0075

	if costEstimate.Input != expectedInput {
		t.Errorf("Expected input cost %f, got %f", expectedInput, costEstimate.Input)
	}

	if costEstimate.Output != expectedOutput {
		t.Errorf("Expected output cost %f, got %f", expectedOutput, costEstimate.Output)
	}

	if costEstimate.Total != expectedTotal {
		t.Errorf("Expected total cost %f, got %f", expectedTotal, costEstimate.Total)
	}
}

func TestModelRegistry(t *testing.T) {
	// Create a test YAML file
	yamlContent := `
models:
  gpt-4o-mini:
    provider: openai
    input_cost_per_million: 0.15
    output_cost_per_million: 0.60
    max_tokens: 128000
    tier: economy
  claude-3-5-sonnet:
    provider: anthropic
    input_cost_per_million: 3.00
    output_cost_per_million: 15.00
    max_tokens: 200000
    tier: premium

router:
  model: "gpt-4o-mini"
  timeout: 5s
`

	tmpFile, err := os.CreateTemp("", "models-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write([]byte(yamlContent)); err != nil {
		t.Fatalf("Failed to write YAML: %v", err)
	}
	_ = tmpFile.Close()

	// Test loading from YAML
	registry := NewRegistry()
	err = registry.LoadFromYAML(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadFromYAML failed: %v", err)
	}

	// Test GetModel
	cfg, exists := registry.GetModel("gpt-4o-mini")
	if !exists {
		t.Error("gpt-4o-mini should exist")
	}
	if cfg.Name != "gpt-4o-mini" {
		t.Errorf("Expected gpt-4o-mini, got %s", cfg.Name)
	}
	if cfg.Provider != "openai" {
		t.Errorf("Expected provider openai, got %s", cfg.Provider)
	}

	// Test GetRouterModel
	routerModel := registry.GetRouterModel()
	if routerModel != "gpt-4o-mini" {
		t.Errorf("Expected router model gpt-4o-mini, got %s", routerModel)
	}

	// Test GetAllModels
	allModels := registry.GetAllModels()
	if len(allModels) != 2 {
		t.Errorf("Expected 2 models, got %d", len(allModels))
	}

	// Test GetModelsByProvider
	openaiModels := registry.GetModelsByProvider("openai")
	if len(openaiModels) != 1 {
		t.Errorf("Expected 1 openai model, got %d", len(openaiModels))
	}

	// Test GetModelsByTier
	economyModels := registry.GetModelsByTier(TierEconomy)
	if len(economyModels) != 1 {
		t.Errorf("Expected 1 economy model, got %d", len(economyModels))
	}

	// Test non-existent model
	_, exists = registry.GetModel("non-existent")
	if exists {
		t.Error("non-existent model should not exist")
	}
}

func TestRegistryConcurrency(t *testing.T) {
	registry := NewRegistry()

	// Test concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			registry.GetAllModels()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without deadlock, test passes
}

// Package config provides configuration loading for the gateway.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelConfig holds configuration for a single LLM model
type ModelConfig struct {
	Provider             string  `yaml:"provider"`
	InputCostPerMillion  float64 `yaml:"input_cost_per_million"`
	OutputCostPerMillion float64 `yaml:"output_cost_per_million"`
	MaxTokens            int     `yaml:"max_tokens"`
	Tier                 string  `yaml:"tier"`
}

// RouterConfig holds router configuration
type RouterConfig struct {
	Model      string `yaml:"model"`
	Timeout    string `yaml:"timeout"`
	MaxRetries int    `yaml:"max_retries"`
}

// ModelsConfig holds the LLM models and router configuration
type ModelsConfig struct {
	Models map[string]*ModelConfig `yaml:"models"`
	Router RouterConfig            `yaml:"router"`
}

// LoadModelsConfig loads model and router configuration from a YAML file
func LoadModelsConfig(path string) (*ModelsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ModelsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate router model exists
	if config.Router.Model != "" {
		if _, exists := config.Models[config.Router.Model]; !exists {
			return nil, fmt.Errorf("router model %q not found in models", config.Router.Model)
		}
	}

	return &config, nil
}

// GetModel returns a model configuration by name
func (c *ModelsConfig) GetModel(name string) (*ModelConfig, bool) {
	cfg, exists := c.Models[name]
	return cfg, exists
}

// GetAllModels returns all model names
func (c *ModelsConfig) GetAllModels() []string {
	names := make([]string, 0, len(c.Models))
	for name := range c.Models {
		names = append(names, name)
	}
	return names
}

// GetModelsByProvider returns all models for a specific provider
func (c *ModelsConfig) GetModelsByProvider(provider string) []*ModelConfig {
	var models []*ModelConfig
	for _, cfg := range c.Models {
		if cfg.Provider == provider {
			models = append(models, cfg)
		}
	}
	return models
}

// GetModelsByTier returns all models in a specific tier
func (c *ModelsConfig) GetModelsByTier(tier string) []*ModelConfig {
	var models []*ModelConfig
	for _, cfg := range c.Models {
		if cfg.Tier == tier {
			models = append(models, cfg)
		}
	}
	return models
}

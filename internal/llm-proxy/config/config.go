// Package config provides proxy configuration loading.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the proxy configuration
type Config struct {
	Models map[string]*ModelConfig `yaml:"models"`
	Router RouterConfig            `yaml:"router"`
}

// ModelConfig holds configuration for a single model
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

// LoadConfig loads proxy configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
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
func (c *Config) GetModel(name string) (*ModelConfig, bool) {
	cfg, exists := c.Models[name]
	return cfg, exists
}

// GetAllModels returns all model names
func (c *Config) GetAllModels() []string {
	names := make([]string, 0, len(c.Models))
	for name := range c.Models {
		names = append(names, name)
	}
	return names
}

// GetModelsByProvider returns all models for a specific provider
func (c *Config) GetModelsByProvider(provider string) []*ModelConfig {
	var models []*ModelConfig
	for _, cfg := range c.Models {
		if cfg.Provider == provider {
			models = append(models, cfg)
		}
	}
	return models
}

// GetModelsByTier returns all models in a specific tier
func (c *Config) GetModelsByTier(tier string) []*ModelConfig {
	var models []*ModelConfig
	for _, cfg := range c.Models {
		if cfg.Tier == tier {
			models = append(models, cfg)
		}
	}
	return models
}

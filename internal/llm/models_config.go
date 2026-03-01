package llm

import (
	"encoding/json"
	"fmt"
	"os"
)

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGoogle    Provider = "google"
	ProviderGrok      Provider = "grok"
)

type ModelConfig struct {
	InputCostPerMillion  float64 `json:"input_cost_per_million"`
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
	MaxTokens            int     `json:"max_tokens"`
	Tier                 string  `json:"tier"`
}

type ProviderConfig struct {
	APIKeyEnv string                  `json:"api_key_env"`
	BaseURL   string                  `json:"base_url,omitempty"`
	Models    map[string]*ModelConfig `json:"models"`
}

type NotDiamondConfig struct {
	APIKeyEnv string `json:"api_key_env"`
	BaseURL   string `json:"base_url"`
}

type RouterConfig struct {
	Model      string `json:"model"`
	Timeout    string `json:"timeout"`
	MaxRetries int    `json:"max_retries"`
}

type ModelsConfig struct {
	Providers  map[string]*ProviderConfig `json:"providers"`
	NotDiamond *NotDiamondConfig          `json:"not_diamond"`
	Router     RouterConfig               `json:"router"`
}

type RuntimeModelConfig struct {
	Provider  Provider
	ModelID   string
	BaseURL   string
	APIKeyEnv string
}

func (c *RuntimeModelConfig) GetAPIKey() string {
	return os.Getenv(c.APIKeyEnv)
}

func LoadModelsConfig(path string) (*ModelsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if config.Router.Model != "" {
		if _, exists := config.GetModel(config.Router.Model); !exists {
			return nil, fmt.Errorf("router model %q not found in models", config.Router.Model)
		}
	}

	return &config, nil
}

func (c *ModelsConfig) GetModel(name string) (*ModelConfig, bool) {
	for _, provider := range c.Providers {
		if model, exists := provider.Models[name]; exists {
			return model, true
		}
	}
	return nil, false
}

func (c *ModelsConfig) GetAllModels() []string {
	names := make([]string, 0)
	for _, provider := range c.Providers {
		for name := range provider.Models {
			names = append(names, name)
		}
	}
	return names
}

func (c *ModelsConfig) GetProvider(name string) (*ProviderConfig, bool) {
	provider, exists := c.Providers[name]
	return provider, exists
}

func (c *ModelsConfig) GetModelsForProvider(providerName string) []*ModelConfig {
	var models []*ModelConfig
	if provider, exists := c.Providers[providerName]; exists {
		for _, model := range provider.Models {
			models = append(models, model)
		}
	}
	return models
}

func (c *ModelsConfig) GetModelsByTier(tier string) []*ModelConfig {
	var models []*ModelConfig
	for _, provider := range c.Providers {
		for _, model := range provider.Models {
			if model.Tier == tier {
				models = append(models, model)
			}
		}
	}
	return models
}

func (c *ModelsConfig) GetNotDiamondAPIKey() string {
	if c.NotDiamond == nil || c.NotDiamond.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.NotDiamond.APIKeyEnv)
}

func (c *ModelsConfig) GetNotDiamondBaseURL() string {
	if c.NotDiamond == nil {
		return ""
	}
	return c.NotDiamond.BaseURL
}

type LLM struct {
	Config        *ModelsConfig
	ModelSelector *ModelSelector
}

func InitLLMModels(path string) (*LLM, error) {
	cfg, err := LoadModelsConfig(path)
	if err != nil {
		return nil, err
	}

	selector := NewModelSelector(cfg.GetNotDiamondBaseURL(), cfg.GetNotDiamondAPIKey())

	return &LLM{
		Config:        cfg,
		ModelSelector: selector,
	}, nil
}

func (c *ModelsConfig) GetRuntimeModel(modelName string) (*RuntimeModelConfig, bool) {
	for providerName, provider := range c.Providers {
		if _, exists := provider.Models[modelName]; exists {
			return &RuntimeModelConfig{
				Provider:  Provider(providerName),
				ModelID:   modelName,
				BaseURL:   provider.BaseURL,
				APIKeyEnv: provider.APIKeyEnv,
			}, true
		}
	}
	return nil, false
}

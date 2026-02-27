package llm

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nevaria-ai/gateway/config"
)

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGoogle    Provider = "google"
	ProviderGrok      Provider = "grok"
)

type ModelConfig struct {
	Provider  Provider `json:"provider"`
	ModelID   string   `json:"model_id"`
	BaseURL   string   `json:"base_url"`
	APIKeyEnv string   `json:"api_key_env"`
}

func (c *ModelConfig) GetAPIKey() string {
	return os.Getenv(c.APIKeyEnv)
}

type ModelsConfig struct {
	Models map[string]*ModelConfig `json:"models"`
}

func LoadModelConfig(path string) (*ModelsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if config.Models == nil {
		config.Models = make(map[string]*ModelConfig)
	}

	return &config, nil
}

func ConvertConfig(cfg *config.ModelsConfig) *ModelsConfig {
	result := &ModelsConfig{
		Models: make(map[string]*ModelConfig),
	}

	for providerName, providerCfg := range cfg.Providers {
		for modelName := range providerCfg.Models {
			result.Models[modelName] = &ModelConfig{
				Provider:  Provider(providerName),
				ModelID:   modelName,
				BaseURL:   providerCfg.BaseURL,
				APIKeyEnv: providerCfg.APIKeyEnv,
			}
		}
	}

	return result
}

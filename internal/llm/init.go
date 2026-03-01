package llm

import (
	"fmt"
	"os"

	"github.com/nevaria-ai/gateway/config"
)

type LLMConfig struct {
	Models         *ModelsConfig
	ModelSelector  *ModelSelector
	ProviderAPIKey map[Provider]string
}

func InitModelsConfig(path string) (*config.ModelsConfig, error) {
	cfg, err := config.LoadModelsConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load models config: %w", err)
	}
	return cfg, nil
}

func InitNotDiamondModelSelector(cfg *config.ModelsConfig) *ModelSelector {
	return NewModelSelector(cfg.GetNotDiamondBaseURL(), cfg.GetNotDiamondAPIKey())
}

func InitLLM(modelsPath string) (*LLMConfig, error) {
	cfg, err := InitModelsConfig(modelsPath)
	if err != nil {
		return nil, err
	}

	models := ConvertConfig(cfg)
	selector := InitNotDiamondModelSelector(cfg)
	providerKeys := extractProviderAPIKeys(cfg)

	return &LLMConfig{
		Models:         models,
		ModelSelector:  selector,
		ProviderAPIKey: providerKeys,
	}, nil
}

func extractProviderAPIKeys(cfg *config.ModelsConfig) map[Provider]string {
	keys := make(map[Provider]string)
	for providerName, providerCfg := range cfg.Providers {
		if providerCfg.APIKeyEnv != "" {
			keys[Provider(providerName)] = os.Getenv(providerCfg.APIKeyEnv)
		}
	}
	return keys
}

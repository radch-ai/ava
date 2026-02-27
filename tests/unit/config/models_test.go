package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevaria-ai/gateway/config"
)

func TestLoadModelsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "base_url": "https://api.openai.com/v1",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        }
      }
    },
    "anthropic": {
      "api_key_env": "ANTHROPIC_API_KEY",
      "models": {
        "claude-3-haiku": {
          "input_cost_per_million": 0.25,
          "output_cost_per_million": 1.25,
          "max_tokens": 200000,
          "tier": "economy"
        }
      }
    }
  },
  "not_diamond": {
    "api_key_env": "NOTDIAMOND_API_KEY",
    "base_url": "https://api.notdiamond.ai"
  },
  "router": {
    "model": "gpt-4o-mini",
    "timeout": "5s",
    "max_retries": 2
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("LoadModelsConfig returned nil")
	}

	if cfg.Router.Model != "gpt-4o-mini" {
		t.Errorf("Expected router model gpt-4o-mini, got %s", cfg.Router.Model)
	}

	if cfg.NotDiamond == nil {
		t.Fatal("NotDiamond config is nil")
	}

	if cfg.NotDiamond.APIKeyEnv != "NOTDIAMOND_API_KEY" {
		t.Errorf("Expected NOTDIAMOND_API_KEY, got %s", cfg.NotDiamond.APIKeyEnv)
	}
}

func TestGetModel(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        }
      }
    }
  },
  "router": {
    "model": "gpt-4o-mini"
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	model, ok := cfg.GetModel("gpt-4o-mini")
	if !ok {
		t.Fatal("GetModel returned false for existing model")
	}

	if model.InputCostPerMillion != 0.15 {
		t.Errorf("Expected input cost 0.15, got %f", model.InputCostPerMillion)
	}
}

func TestGetAllModels(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        }
      }
    },
    "anthropic": {
      "api_key_env": "ANTHROPIC_API_KEY",
      "models": {
        "claude-3-haiku": {
          "input_cost_per_million": 0.25,
          "output_cost_per_million": 1.25,
          "max_tokens": 200000,
          "tier": "economy"
        }
      }
    }
  },
  "router": {
    "model": "gpt-4o-mini"
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	models := cfg.GetAllModels()
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}
}

func TestGetProviderConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "base_url": "https://api.openai.com/v1",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        }
      }
    }
  },
  "router": {
    "model": "gpt-4o-mini"
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	provider, ok := cfg.GetProvider("openai")
	if !ok {
		t.Fatal("GetProvider returned false for existing provider")
	}

	if provider.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("Expected OPENAI_API_KEY, got %s", provider.APIKeyEnv)
	}

	if provider.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("Expected base_url https://api.openai.com/v1, got %s", provider.BaseURL)
	}
}

func TestGetModelsForProvider(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        },
        "gpt-4o": {
          "input_cost_per_million": 2.50,
          "output_cost_per_million": 10.00,
          "max_tokens": 128000,
          "tier": "premium"
        }
      }
    },
    "anthropic": {
      "api_key_env": "ANTHROPIC_API_KEY",
      "models": {
        "claude-3-haiku": {
          "input_cost_per_million": 0.25,
          "output_cost_per_million": 1.25,
          "max_tokens": 200000,
          "tier": "economy"
        }
      }
    }
  },
  "router": {
    "model": "gpt-4o-mini"
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
	if err != nil {
		t.Fatalf("LoadModelsConfig failed: %v", err)
	}

	openaiModels := cfg.GetModelsForProvider("openai")
	if len(openaiModels) != 2 {
		t.Errorf("Expected 2 openai models, got %d", len(openaiModels))
	}

	anthropicModels := cfg.GetModelsForProvider("anthropic")
	if len(anthropicModels) != 1 {
		t.Errorf("Expected 1 anthropic model, got %d", len(anthropicModels))
	}
}

func TestGetModelsByTier(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "models.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "models": {
        "gpt-4o-mini": {
          "input_cost_per_million": 0.15,
          "output_cost_per_million": 0.60,
          "max_tokens": 128000,
          "tier": "economy"
        },
        "gpt-4o": {
          "input_cost_per_million": 2.50,
          "output_cost_per_million": 10.00,
          "max_tokens": 128000,
          "tier": "premium"
        }
      }
    }
  },
  "router": {
    "model": "gpt-4o-mini"
  }
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := config.LoadModelsConfig(configPath)
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

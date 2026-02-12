package models

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry holds all registered models from config
type Registry struct {
	mu     sync.RWMutex
	models map[string]*ModelConfig // key: model name
	router RouterConfig
}

// RouterConfig holds configuration for the router model
type RouterConfig struct {
	Model   string `yaml:"model"`
	Timeout string `yaml:"timeout"`
}

var (
	globalRegistry *Registry
	once           sync.Once
)

// NewRegistry creates a new model registry
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]*ModelConfig),
	}
}

// Get returns the global model registry (singleton)
func Get() *Registry {
	once.Do(func() {
		globalRegistry = NewRegistry()
	})
	return globalRegistry
}

// LoadFromYAML loads model and router configuration from YAML file
func (r *Registry) LoadFromYAML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var config struct {
		Models map[string]*ModelConfig `yaml:"models"`
		Router RouterConfig            `yaml:"router"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Store models and set their names
	for name, cfg := range config.Models {
		cfg.Name = name
		r.models[name] = cfg
	}

	// Store router config
	r.router = config.Router

	return nil
}

// GetModel returns a model configuration by name
func (r *Registry) GetModel(name string) (*ModelConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, exists := r.models[name]
	return cfg, exists
}

// GetRouterModel returns the configured router model name
func (r *Registry) GetRouterModel() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.router.Model
}

// GetAllModels returns all registered model names
func (r *Registry) GetAllModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.models))
	for name := range r.models {
		names = append(names, name)
	}
	return names
}

// GetModelsByProvider returns all models for a specific provider
func (r *Registry) GetModelsByProvider(provider string) []*ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var models []*ModelConfig
	for _, cfg := range r.models {
		if cfg.Provider == provider {
			models = append(models, cfg)
		}
	}
	return models
}

// GetModelsByTier returns all models in a specific tier
func (r *Registry) GetModelsByTier(tier ModelTier) []*ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var models []*ModelConfig
	for _, cfg := range r.models {
		if cfg.Tier == tier {
			models = append(models, cfg)
		}
	}
	return models
}

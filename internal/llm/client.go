// Package llm provides a simplified LLM client that connects to llm-proxy.
//
// The gateway no longer handles quota or model routing directly.
// Instead, it connects to llm-proxy which exposes an OpenAI-compatible API.
package llm

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// Config holds the LLM client configuration.
type Config struct {
	// BaseURL is the llm-proxy endpoint (e.g., "http://localhost:8080/v1")
	BaseURL string
	// APIKey is the API key for authentication (can be user-specific or service key)
	APIKey string
	// DefaultModel is the model to use when none specified (can be "auto" for routing)
	DefaultModel string
}

// Client creates and caches LLM chat models.
// Models are cached by name to avoid repeated initialization.
type Client struct {
	Config    *Config
	cache     map[string]model.ToolCallingChatModel
	cacheLock sync.RWMutex
}

// NewClient creates a new LLM client.
func NewClient(cfg *Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = getEnv("LLM_PROXY_URL", "http://localhost:8080/v1")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = getEnv("LLM_PROXY_API_KEY", "")
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "auto"
	}

	return &Client{
		Config: cfg,
		cache:  make(map[string]model.ToolCallingChatModel),
	}
}

// CreateChatModel creates or returns a cached chat model for the given model name.
// Pass "auto" to let llm-proxy handle model routing.
func (c *Client) CreateChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
	if modelName == "" {
		modelName = c.Config.DefaultModel
	}

	c.cacheLock.RLock()
	cached, exists := c.cache[modelName]
	c.cacheLock.RUnlock()

	if exists {
		return cached, nil
	}

	chatCfg := &openai.ChatModelConfig{
		BaseURL: c.Config.BaseURL,
		APIKey:  c.Config.APIKey,
		Model:   modelName,
	}

	chatModel, err := openai.NewChatModel(ctx, chatCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model for %s: %w", modelName, err)
	}

	c.cacheLock.Lock()
	c.cache[modelName] = chatModel
	c.cacheLock.Unlock()

	return chatModel, nil
}

// ClearCache clears the model cache.
func (c *Client) ClearCache() {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	c.cache = make(map[string]model.ToolCallingChatModel)
}

// GetCachedModels returns the names of cached models.
func (c *Client) GetCachedModels() []string {
	c.cacheLock.RLock()
	defer c.cacheLock.RUnlock()

	models := make([]string, 0, len(c.cache))
	for name := range c.cache {
		models = append(models, name)
	}
	return models
}

// DefaultConfig returns a Config populated from environment variables.
func DefaultConfig() *Config {
	return &Config{
		BaseURL:      getEnv("LLM_PROXY_URL", "http://localhost:8080/v1"),
		APIKey:       getEnv("LLM_PROXY_API_KEY", ""),
		DefaultModel: getEnv("LLM_DEFAULT_MODEL", "auto"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

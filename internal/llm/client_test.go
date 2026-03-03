package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient_Defaults(t *testing.T) {
	cfg := &Config{}
	client := NewClient(cfg)

	assert.NotNil(t, client)
	assert.NotEmpty(t, client.Config.BaseURL)
	assert.Equal(t, "auto", client.Config.DefaultModel)
}

func TestNewClient_WithConfig(t *testing.T) {
	cfg := &Config{
		BaseURL:      "http://custom-proxy:9000/v1",
		APIKey:       "test-key",
		DefaultModel: "gpt-4o",
	}
	client := NewClient(cfg)

	assert.Equal(t, "http://custom-proxy:9000/v1", client.Config.BaseURL)
	assert.Equal(t, "test-key", client.Config.APIKey)
	assert.Equal(t, "gpt-4o", client.Config.DefaultModel)
}

func TestClient_ClearCache(t *testing.T) {
	client := NewClient(&Config{})
	assert.NotNil(t, client.cache)

	client.ClearCache()
	assert.Empty(t, client.cache)
}

func TestClient_GetCachedModels(t *testing.T) {
	client := NewClient(&Config{})
	assert.Empty(t, client.GetCachedModels())
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.BaseURL)
	assert.Equal(t, "auto", cfg.DefaultModel)
}

package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"google.golang.org/genai"
)

// ProviderFactory creates chat model instances with caching.
// Models are cached after first creation to avoid repeated initialization.
type ProviderFactory struct {
	config    *ModelsConfig
	cache     map[string]model.ToolCallingChatModel
	cacheLock sync.RWMutex
}

func NewProviderFactory(config *ModelsConfig) *ProviderFactory {
	return &ProviderFactory{
		config: config,
		cache:  make(map[string]model.ToolCallingChatModel),
	}
}

// CreateChatModel creates or returns a cached chat model for the given model name.
// Supported providers: openai, anthropic, google, grok.
func (f *ProviderFactory) CreateChatModel(ctx context.Context, modelName string) (model.ToolCallingChatModel, error) {
	f.cacheLock.RLock()
	cached, exists := f.cache[modelName]
	f.cacheLock.RUnlock()

	if exists {
		return cached, nil
	}

	modelConfig, ok := f.config.GetRuntimeModel(modelName)
	if !ok {
		return nil, fmt.Errorf("model %s not found in config", modelName)
	}

	apiKey := modelConfig.GetAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found for model %s (env var: %s)", modelName, modelConfig.APIKeyEnv)
	}

	var chatModel model.ToolCallingChatModel
	var err error

	switch modelConfig.Provider {
	case ProviderOpenAI:
		chatModel, err = f.createOpenAIModel(ctx, modelConfig, apiKey)
	case ProviderAnthropic:
		chatModel, err = f.createAnthropicModel(ctx, modelConfig, apiKey)
	case ProviderGoogle:
		chatModel, err = f.createGeminiModel(ctx, modelConfig, apiKey)
	case ProviderGrok:
		chatModel, err = f.createGrokModel(ctx, modelConfig, apiKey)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", modelConfig.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create chat model for %s: %w", modelName, err)
	}

	f.cacheLock.Lock()
	f.cache[modelName] = chatModel
	f.cacheLock.Unlock()

	return chatModel, nil
}

func (f *ProviderFactory) createOpenAIModel(ctx context.Context, config *RuntimeModelConfig, apiKey string) (model.ToolCallingChatModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for OpenAI provider")
	}

	cfg := &openai.ChatModelConfig{
		APIKey: apiKey,
		Model:  config.ModelID,
	}

	if config.BaseURL != "" {
		cfg.BaseURL = config.BaseURL
	}

	chatModel, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI chat model: %w", err)
	}

	return chatModel, nil
}

func (f *ProviderFactory) createAnthropicModel(ctx context.Context, config *RuntimeModelConfig, apiKey string) (model.ToolCallingChatModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Anthropic provider")
	}

	cfg := &claude.Config{
		APIKey:    apiKey,
		Model:     config.ModelID,
		MaxTokens: 4096,
	}

	if config.BaseURL != "" {
		cfg.BaseURL = &config.BaseURL
	}

	chatModel, err := claude.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Anthropic chat model: %w", err)
	}

	return chatModel, nil
}

func (f *ProviderFactory) createGeminiModel(ctx context.Context, config *RuntimeModelConfig, apiKey string) (model.ToolCallingChatModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Google provider")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	cfg := &gemini.Config{
		Client: client,
		Model:  config.ModelID,
	}

	chatModel, err := gemini.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini chat model: %w", err)
	}

	return chatModel, nil
}

func (f *ProviderFactory) createGrokModel(ctx context.Context, config *RuntimeModelConfig, apiKey string) (model.ToolCallingChatModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Grok provider")
	}

	cfg := &openai.ChatModelConfig{
		APIKey: apiKey,
		Model:  config.ModelID,
	}

	if config.BaseURL != "" {
		cfg.BaseURL = config.BaseURL
	} else {
		cfg.BaseURL = "https://api.x.ai/v1"
	}

	chatModel, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Grok chat model: %w", err)
	}

	return chatModel, nil
}

func (f *ProviderFactory) ClearCache() {
	f.cacheLock.Lock()
	defer f.cacheLock.Unlock()
	f.cache = make(map[string]model.ToolCallingChatModel)
}

func (f *ProviderFactory) GetCachedModels() []string {
	f.cacheLock.RLock()
	defer f.cacheLock.RUnlock()

	models := make([]string, 0, len(f.cache))
	for name := range f.cache {
		models = append(models, name)
	}
	return models
}

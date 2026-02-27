package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nevaria-ai/gateway/config"
	"github.com/nevaria-ai/gateway/internal/llm"
	"github.com/nevaria-ai/gateway/internal/server"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config validation error: %v\n", err)
		os.Exit(1)
	}

	// Configure logger level
	logLevel, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level %q: %v\n", cfg.LogLevel, err)
		os.Exit(1)
	}
	zerolog.SetGlobalLevel(logLevel)

	// Create logger
	logger := zerolog.New(os.Stderr).With().Timestamp().Str("service", "nevaria-gateway").Logger()

	// For development, allow all origins if none specified.
	// In production, always set GATEWAY_ALLOWED_ORIGINS environment variable.
	allowedOrigins := cfg.AllowedOrigins
	if len(allowedOrigins) == 0 {
		logger.Warn().Msg("GATEWAY_ALLOWED_ORIGINS not set, allowing all origins - configure this for production!")
		allowedOrigins = []string{"*"}
	}

	// Use configured max connections or default to 10000
	maxConns := cfg.MaxConns
	if maxConns == 0 {
		maxConns = 10000
		logger.Info().Int("max_conns", maxConns).Msg("using default connection limit")
	}

	// Create Redis client (required - for session caching and quota)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	logger.Info().Str("addr", cfg.RedisAddr).Msg("Redis client initialized")

	// Load models configuration
	modelsCfg, err := config.LoadModelsConfig("config/models.json")
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load models config, continuing without LLM support")
		modelsCfg = nil
	} else {
		logger.Info().Msg("models configuration loaded")
	}

	// Create model selector and Eino client
	var einoClient *llm.EinoClient
	if modelsCfg != nil {
		var ndAPIKey string
		if modelsCfg.NotDiamond != nil {
			ndAPIKey = os.Getenv(modelsCfg.NotDiamond.APIKeyEnv)
		}

		selector := llm.NewModelSelector(modelsCfg.NotDiamond.BaseURL, ndAPIKey)
		llmModels := llm.ConvertConfig(modelsCfg)
		einoClient = llm.NewEinoClient(llmModels, selector, redisClient)
		logger.Info().Msg("Eino client initialized")
	}

	// Create Redis cache adapter
	cache := server.NewRedisCache(redisClient)

	// Create auth middleware with cache support
	// Default cache TTL to 5 minutes if not specified
	authMW := server.NewAuthMiddleware(cfg.ZetidalURL, cache, 5*time.Minute)

	// Create WebSocket handler with Eino client
	wsHandler := websocket.NewHandler(logger, allowedOrigins, maxConns, &websocket.AgentManagerOptions{
		LLMClient: einoClient,
	})

	// Create and start server with auth middleware
	srv := server.New(cfg.ServerAddr, logger, wsHandler, authMW)
	if err := srv.Start(); err != nil {
		logger.Error().Err(err).Msg("server failed")
		os.Exit(1)
	}
}

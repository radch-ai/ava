package main

import (
	"context"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/gateway/config"
	"github.com/nevaria-ai/gateway/internal/llm"
	"github.com/nevaria-ai/gateway/internal/server"
	"github.com/nevaria-ai/gateway/internal/websocket"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
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

	// Warn on insecure origins configuration:
	// - Empty origins denies all requests (secure but likely misconfigured)
	// - Wildcard "*" allows all (insecure for production)
	// - Warnings logged to force explicit production configuration
	if len(cfg.AllowedOrigins) == 0 {
		logger.Warn().Msg("GATEWAY_ALLOWED_ORIGINS is empty - all requests will be denied")
	} else if len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*" {
		logger.Warn().Msg("GATEWAY_ALLOWED_ORIGINS='*' allows all origins - not recommended for production!")
	}

	// Initialize LLM client (connects to llm-proxy):
	// - llm-proxy handles quota, model routing, and multi-provider support
	// - Gateway just sends requests to llm-proxy's OpenAI-compatible endpoint
	llmClient := llm.NewClient(llm.DefaultConfig())
	logger.Info().
		Str("proxy_url", llmClient.Config.BaseURL).
		Str("default_model", llmClient.Config.DefaultModel).
		Msg("LLM client initialized")

	// Initialize Redis client:
	// - Ensures Redis connection is configured before server starts
	// - TODO: Wire to storage/user config store
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	_ = rdb // TODO: Wire to storage/user config store

	// Create WebSocket handler:
	// - AgentManager accepts nil opts and operates without LLM until configured
	// - ChatModel will be created per-session from llmClient
	wsHandler := websocket.NewHandler(logger, cfg.AllowedOrigins, cfg.MaxConns, nil)

	// Create Zitadel introspector:
	zitadelIntrospector, err := server.NewZitadelIntrospector(context.Background(), cfg.ZitadelDomain, cfg.ZitadelKeyPath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create zitadel introspector")
		os.Exit(1)
	}

	// Create and start server
	srv := server.New(cfg.ServerAddr, logger, wsHandler, zitadelIntrospector)
	if err := srv.Start(); err != nil {
		logger.Error().Err(err).Msg("server failed")
		os.Exit(1)
	}
}

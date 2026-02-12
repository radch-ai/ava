package main

import (
	"fmt"
	"os"

	"github.com/nevaria-ai/gateway/internal/config"
	"github.com/nevaria-ai/gateway/internal/proxy/models"
	"github.com/nevaria-ai/gateway/internal/server"
	"github.com/nevaria-ai/gateway/internal/websocket"
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
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Initialize model registry
	registry := models.Get()
	modelConfigPath := os.Getenv("GATEWAY_MODEL_CONFIG")
	if modelConfigPath == "" {
		modelConfigPath = "config/models.yaml"
	}

	if err := registry.LoadFromYAML(modelConfigPath); err != nil {
		logger.Warn().Err(err).Str("path", modelConfigPath).Msg("failed to load model configuration")
	} else {
		routerModel := registry.GetRouterModel()
		logger.Info().Str("router_model", routerModel).Msg("model registry initialized")
	}

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

	// Create WebSocket handler with config
	wsHandler := websocket.NewHandler(logger, allowedOrigins, maxConns)

	// Create and start server
	srv := server.New(cfg.ServerAddr, logger, wsHandler)
	if err := srv.Start(); err != nil {
		logger.Error().Err(err).Msg("server failed")
		os.Exit(1)
	}
}

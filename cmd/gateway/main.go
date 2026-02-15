package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nevaria-ai/gateway/config"
	"github.com/nevaria-ai/gateway/internal/llm-proxy/models"
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

	// Create Redis client (optional - for session caching and quota)
	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		})
		logger.Info().Str("addr", cfg.RedisAddr).Msg("Redis client initialized")
	}

	// Create Redis cache adapter (reuse existing Redis client)
	var cache server.Cache
	if redisClient != nil {
		cache = server.NewRedisCache(redisClient)
	} else {
		logger.Warn().Msg("Redis not configured, session caching disabled")
	}

	// Create auth middleware with cache support
	// Default cache TTL to 5 minutes if not specified
	authMW := server.NewAuthMiddleware(cfg.Auth.URL, cfg.Auth.AuthInternalSecret, cache, 5*time.Minute)

	// Create WebSocket handler with config
	wsHandler := websocket.NewHandler(logger, allowedOrigins, maxConns)

	// Create internal router for auth service webhooks with cache support
	// Uses gateway's own INTERNAL_SECRET to protect endpoints
	internalRouter := server.NewInternalRouter(cfg.Auth.InternalSecret, cache)

	// Create and start server with auth middleware
	srv := server.New(cfg.ServerAddr, logger, wsHandler, authMW, internalRouter)
	if err := srv.Start(); err != nil {
		logger.Error().Err(err).Msg("server failed")
		os.Exit(1)
	}
}

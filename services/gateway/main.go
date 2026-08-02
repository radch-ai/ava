package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	pkgconfig "github.com/radch-ai/ava/pkg/config"
	"github.com/radch-ai/ava/pkg/zitadelvalidate"

	"github.com/radch-ai/ava/services/gateway/config"
	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/server"
	"github.com/radch-ai/ava/services/gateway/internal/websocket"
)

// Version is set at build time via -ldflags `-X main.Version=<tag>`.
var Version = "dev"

func main() {
	// Setup logging first (reads log level from env)
	logger := pkgconfig.SetupLogger("ava-gateway", "GATEWAY")

	logger.Info().Str("version", Version).Msg("starting ava-gateway")

	// Load configuration (uses logger for fatal errors)
	cfg := config.Load(logger)

	// Create cancellable context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize database connection pool
	dbPool, err := newDBPool(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer dbPool.Close()

	// Initialize Redis client for fast lookups and pub/sub messaging.
	// Scheduler service publishes task events; gateway subscribes and executes.
	// TODO: Wire pub/sub handler for scheduler -> gateway communication
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	_ = rdb // TODO: Wire pub/sub handler

	// Warn on insecure origins configuration:
	// - Empty origins denies all requests (secure but likely misconfigured)
	// - Wildcard "*" allows all (insecure for production)
	// - Warnings logged to force explicit production configuration
	if len(cfg.AllowedOrigins) == 0 {
		logger.Warn().Msg("GATEWAY_ALLOWED_ORIGINS is empty - all requests will be denied")
	} else if len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*" {
		logger.Warn().Msg("GATEWAY_ALLOWED_ORIGINS='*' allows all origins - not recommended for production!")
	}

	logger.Info().
		Str("bifrost_url", cfg.BifrostBaseURL).
		Str("database", maskURL(cfg.DatabaseURL)).
		Str("redis", cfg.RedisAddr).
		Msg("Configuration loaded")

	// Create WebSocket handler:
	// - UserAgentManager creates agents with Bifrost connection
	// - Model selection ("auto") is handled by Bifrost
	wsHandler := websocket.NewHandler(logger, cfg.AllowedOrigins, cfg.MaxConns, &agent.UserAgentManagerOptions{
		BifrostBaseURL: cfg.BifrostBaseURL,
		DBPool:         dbPool,
	})

	// Create Zitadel introspector for session validation (panics on error)
	zitadelIntr := zitadelvalidate.MustNewIntrospector(ctx, cfg.ZitadelDomain, cfg.ZitadelKeyPath).WithLogger(logger)

	// Create and start server
	srv := server.New(cfg.ServerAddr, logger, wsHandler, zitadelIntr)
	if err := srv.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
}

// maskURL masks sensitive parts of a URL for safe logging.
// Shows first 10 and last 10 characters with *** in between.
func maskURL(url string) string {
	if len(url) < 20 {
		return "***"
	}
	return url[:10] + "***" + url[len(url)-10:]
}

// newDBPool creates a tuned PostgreSQL connection pool.
func newDBPool(ctx context.Context, databaseURL string, logger zerolog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	// Tune for gateway service
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = 1 * time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Verify connection is alive at startup
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	logger.Info().
		Int("max_conns", int(poolCfg.MaxConns)).
		Int("min_conns", int(poolCfg.MinConns)).
		Msg("Database pool created")

	return pool, nil
}

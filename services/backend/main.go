package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	pkgconfig "github.com/radch-ai/ava/pkg/config"
	"github.com/radch-ai/ava/pkg/zitadelvalidate"
	"github.com/radch-ai/ava/services/backend/config"
	"github.com/radch-ai/ava/services/backend/internal/api"
	"github.com/radch-ai/ava/services/backend/internal/server"
	"github.com/radch-ai/ava/services/backend/internal/webhooks"
)

// Version is set at build time via -ldflags `-X main.Version=<tag>`.
var Version = "dev"

func main() {
	// Setup logging first (reads log level from env)
	logger := pkgconfig.SetupLogger("ava-backend", "BACKEND")

	logger.Info().Str("version", Version).Msg("starting ava-backend")

	// Load configuration
	cfg := config.Load(logger)

	// Create cancellable context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info().
		Str("database", maskURL(cfg.DatabaseURL)).
		Msg("Configuration loaded")

	// Initialize database connection pool
	dbPool, err := newDBPool(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer dbPool.Close()

	// Initialize auth
	zitadelIntr := zitadelvalidate.MustNewIntrospector(ctx, cfg.ZitadelDomain, cfg.ZitadelIntrospectorPrivateKeyPath).WithLogger(logger)

	// Initialize API and webhooks
	apiRouter := api.New(dbPool, logger)
	webhookRouter := webhooks.New(cfg, dbPool, logger)

	// Create and start server
	srv := server.New(logger, cfg.ServerAddr, zitadelIntr, apiRouter, webhookRouter)
	if err := srv.Start(ctx); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
}

// maskURL masks sensitive parts of a URL for safe logging.
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

	// Tune for backend service
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute

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

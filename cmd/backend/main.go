package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/backend/config"
	"github.com/nevaria-ai/backend/internal/server"
	"github.com/nevaria-ai/backend/internal/user"
	"github.com/nevaria-ai/backend/internal/webhooks"
	"github.com/nevaria-ai/zitadel-validate"
)

func main() {
	// Setup logging first (reads log level from env)
	logger := config.SetupLogger()

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
	zitadelIntr := zitadelvalidate.MustNewIntrospector(ctx, cfg.ZitadelDomain, cfg.ZitadelKeyPath).WithLogger(logger)

	// Initialize services
	userStore := user.NewPGStore(dbPool)
	userService := user.NewService(userStore)
	userHandler := user.NewHandler(userService, logger)

	// Initialize webhooks
	webhookHandlers := webhooks.New(dbPool, logger)

	// Create and start server
	srv := server.New(cfg.ServerAddr, logger, dbPool, zitadelIntr, &server.Handlers{
		UserHandler: userHandler,
	}, &server.WebhookHandlers{
		Router: webhookHandlers.Router(),
	})
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
	poolCfg.MaxConnLifetime = 1 * 60 * 60 * 1000 * 1000 // 1 hour in microseconds
	poolCfg.MaxConnIdleTime = 30 * 60 * 1000 * 1000     // 30 minutes in microseconds
	poolCfg.HealthCheckPeriod = 1 * 60 * 1000 * 1000    // 1 minute in microseconds

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

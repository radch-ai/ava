// Command ava runs the ava server.
package main

import (
	"context"
	"encoding/hex"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/utils"
)

// Version is set at build time via -ldflags `-X main.Version=<tag>`.
var Version = "dev"

// config holds the server-level values the binary itself consumes.
type config struct {
	Addr      string
	DBPath    string
	MasterKey []byte
}

// loadConfig binds env vars onto the config — all env reading lives here.
//
// AVA_DB: SQLite file, required; ":memory:" runs the database
// in-process (dev only — the justfile run recipe defaults to it).
//
// AVA_SECRETS_MASTER_KEY: secret-vault master key, required — see
// .env.example for format and the restart-stability requirement.
func loadConfig(logger zerolog.Logger) config {
	masterHex := utils.GetEnvRequired("AVA_SECRETS_MASTER_KEY", logger)
	masterKey, err := hex.DecodeString(masterHex)
	if err != nil {
		logger.Fatal().Str("env_var", "AVA_SECRETS_MASTER_KEY").
			Msg("must be 64 hex chars (openssl rand -hex 32)")
	}
	if len(masterKey) != 32 {
		logger.Fatal().Str("env_var", "AVA_SECRETS_MASTER_KEY").
			Msg("must decode to 32 bytes (openssl rand -hex 32)")
	}
	return config{
		Addr:      utils.GetEnv("AVA_ADDR", ":8080"),
		DBPath:    utils.GetEnvRequired("AVA_DB", logger),
		MasterKey: masterKey,
	}
}

func main() {
	// Setup logging first (reads log level from env)
	logger := utils.SetupLogger("ava", "AVA")

	logger.Info().Str("version", Version).Msg("starting ava")

	// Load configuration
	cfg := loadConfig(logger)

	// Create cancellable context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info().
		Str("addr", cfg.Addr).
		Msg("Configuration loaded")

	// Open the store here so the backend stays engine-agnostic.
	sqlDB, err := sqlstore.OpenSQLite(cfg.DBPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open store")
	}
	logger.Info().Str("path", cfg.DBPath).Msg("store ready")

	apiHandler, err := backend.New(sqlDB, logger, cfg.MasterKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Vault bootstrap failed")
	}

	wsHandler, err := gateway.New(&gateway.Config{}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create gateway handler")
	}

	// Component mounts: backend under /api, gateway at /ws. The /healthz
	// probe comes from utils.Serve.
	root := http.NewServeMux()
	root.Handle("/api/", apiHandler)
	root.Handle("GET /ws", wsHandler)

	if err := utils.Serve(ctx, root, cfg.Addr, logger); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
}

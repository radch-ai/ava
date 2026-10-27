// Command ava runs the ava server.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/utils"
)

// Version is set at build time via -ldflags `-X main.Version=<tag>`.
var Version = "dev"

// baseConfig holds the server-level values the binary itself consumes.
type baseConfig struct {
	Addr string
}

// loadConfig binds env vars onto the base, backend, and gateway configs —
// all env reading lives here.
func loadConfig(logger zerolog.Logger) (baseConfig, *backend.Config, *gateway.Config) {
	dbPath := utils.GetEnv("AVA_DB", "data/ava.db")
	return baseConfig{
			Addr: utils.GetEnv("AVA_ADDR", ":8080"),
		},
		&backend.Config{DBPath: dbPath},
		&gateway.Config{DBPath: dbPath}
}

func main() {
	// Setup logging first (reads log level from env)
	logger := utils.SetupLogger("ava", "AVA")

	logger.Info().Str("version", Version).Msg("starting ava")

	// Load configuration
	baseCfg, backendCfg, gatewayCfg := loadConfig(logger)

	// Create cancellable context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info().
		Str("addr", baseCfg.Addr).
		Msg("Configuration loaded")

	apiHandler, err := backend.New(backendCfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create backend handler")
	}

	wsHandler, err := gateway.New(gatewayCfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create gateway handler")
	}

	// Component mounts: backend under /api, gateway at /ws. The /healthz
	// probe comes from utils.Serve.
	root := http.NewServeMux()
	root.Handle("/api/", apiHandler)
	root.Handle("GET /ws", wsHandler)

	if err := utils.Serve(ctx, root, baseCfg.Addr, logger); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
}

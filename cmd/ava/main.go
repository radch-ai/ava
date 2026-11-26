// Command ava runs the ava server; its subcommands rotate vault keys.
package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/auth"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/utils"
)

// Version is set at build time via -ldflags `-X main.Version=<tag>`.
var Version = "dev"

// config holds the server-level values the binary itself consumes.
type config struct {
	Addr       string
	DBPath     string
	MasterKey  []byte
	Token      string
	ToolLimits backend.ToolLimits
	// ModelCatalogURL is the models.dev snapshot the context-window
	// catalog refreshes from; empty disables refresh.
	ModelCatalogURL string
}

// loadConfig binds env vars onto the config — all env reading lives here.
//
// AVA_DB: SQLite file, required; ":memory:" runs the database
// in-process (dev only — the justfile run recipe defaults to it).
//
// AVA_SECRETS_MASTER_KEY: secret-vault master key, required — see
// .env.example for format and the restart-stability requirement.
//
// AVA_UI_TOKEN: the UI access token exchanged at login for a session
// cookie, required.
// The AVA_MCP_CONCURRENCY_PER_SERVER and AVA_TOOL_CONCURRENCY_GLOBAL
// bindings are the invocation capacity budget: operator-set limits that
// neither a model nor a server can raise.
//
// AVA_MODEL_CATALOG_URL: the models.dev snapshot the context-window
// catalog refreshes from. Unset takes the upstream default; set but
// empty disables refresh, leaving the embedded snapshot — the switch
// has to be expressible, so this read bypasses GetEnv's
// empty-as-unset rule.
func loadConfig(logger zerolog.Logger) config {
	return config{
		Addr:            utils.GetEnv("AVA_ADDR", ":8080"),
		DBPath:          utils.GetEnvRequired("AVA_DB", logger),
		MasterKey:       masterKeyFromEnv(logger, "AVA_SECRETS_MASTER_KEY"),
		Token:           utils.GetEnvRequired("AVA_UI_TOKEN", logger),
		ToolLimits:      loadToolLimits(logger),
		ModelCatalogURL: modelCatalogURL(),
	}
}

// defaultModelCatalogURL is the vendored snapshot's upstream.
const defaultModelCatalogURL = "https://models.dev/api.json"

// modelCatalogURL answers the catalog source: unset takes the upstream
// default, set-but-empty disables refresh.
func modelCatalogURL() string {
	if raw, ok := os.LookupEnv("AVA_MODEL_CATALOG_URL"); ok {
		return raw
	}
	return defaultModelCatalogURL
}

// loadToolLimits binds the capacity budget, fatally answering a malformed
// value: a silently defaulted limit would weaken a policy bound.
func loadToolLimits(logger zerolog.Logger) backend.ToolLimits {
	defaults := backend.DefaultToolLimits()
	return backend.ToolLimits{
		ConcurrencyPerServer: loadPositiveEnvInt(logger, "AVA_MCP_CONCURRENCY_PER_SERVER", defaults.ConcurrencyPerServer),
		ConcurrencyGlobal:    loadPositiveEnvInt(logger, "AVA_TOOL_CONCURRENCY_GLOBAL", defaults.ConcurrencyGlobal),
	}
}

// loadPositiveEnvInt binds one operator-set numeric bound, fatally
// answering a malformed or non-positive value: a silently defaulted
// limit would weaken a policy bound.
func loadPositiveEnvInt(logger zerolog.Logger, key string, fallback int) int {
	v, err := utils.GetEnvInt(key, fallback)
	if err != nil {
		logger.Fatal().Err(err).Str("env_var", key).Msg("invalid integer environment variable")
	}
	if v <= 0 {
		logger.Fatal().Str("env_var", key).Int("value", v).
			Msg("limit must be positive")
	}
	return v
}

// masterKeyFromEnv reads one master-key env var and parses it.
func masterKeyFromEnv(logger zerolog.Logger, envVar string) []byte {
	key, err := backend.MasterKeyFromHex(utils.GetEnvRequired(envVar, logger))
	if err != nil {
		logger.Fatal().Err(err).Str("env_var", envVar).
			Msg("generate with openssl rand -hex 32")
	}
	return key
}

func main() {
	// Setup logging first (reads log level from env)
	logger := utils.SetupLogger("ava", "AVA")

	// No subcommand: serve. Rotation subcommands manage vault keys.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "rotate-master":
			runRotateMaster(logger)
			return
		case "rotate-keys":
			runRotateKeys(logger)
			return
		default:
			logger.Fatal().Str("subcommand", os.Args[1]).
				Msg("unknown subcommand (rotate-master, rotate-keys)")
		}
	}

	serve(logger)
}

// runRotateMaster re-wraps the vault keyset under a new master key,
// read from an interactive prompt (entries hidden). Afterwards boot
// the server with the new value in AVA_SECRETS_MASTER_KEY.
func runRotateMaster(logger zerolog.Logger) {
	oldKey := masterKeyFromEnv(logger, "AVA_SECRETS_MASTER_KEY")
	newKey, err := backend.PromptNewMasterKey(os.Stdin, os.Stdout)
	if err != nil {
		logger.Fatal().Err(err).Msg("Reading new master key failed")
	}
	if bytes.Equal(oldKey, newKey) {
		logger.Fatal().Msg("new master key equals the current key")
	}
	sqlDB, err := sqlstore.OpenSQLite(utils.GetEnvRequired("AVA_DB", logger))
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open store")
	}
	defer func() { _ = sqlDB.Close() }()

	if err := backend.RotateMaster(sqlDB, oldKey, newKey); err != nil {
		logger.Fatal().Err(err).Msg("Master key rotation failed")
	}
	logger.Info().Msg("master key rotated — set AVA_SECRETS_MASTER_KEY to the new key")
}

// runRotateKeys mints and promotes a fresh vault data key and
// re-seals every stored entry under it.
func runRotateKeys(logger zerolog.Logger) {
	masterKey := masterKeyFromEnv(logger, "AVA_SECRETS_MASTER_KEY")
	sqlDB, err := sqlstore.OpenSQLite(utils.GetEnvRequired("AVA_DB", logger))
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to open store")
	}
	defer func() { _ = sqlDB.Close() }()

	resealed, err := backend.RotateKeys(sqlDB, masterKey)
	if err != nil {
		logger.Fatal().Err(err).Msg("Data key rotation failed")
	}
	logger.Info().Int("resealed", resealed).
		Msg("vault data keys rotated; every entry re-sealed")
}

// serve runs the API server.
func serve(logger zerolog.Logger) {
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

	// The catalog's cache lives beside the database: the directory is
	// the one this process owns and already exists for a file-backed
	// store. An in-memory database has no such directory.
	catalogCache := ""
	if cfg.DBPath != ":memory:" {
		catalogCache = filepath.Join(filepath.Dir(cfg.DBPath), "model-catalog.json")
	}
	catalog, err := backend.LoadModelCatalog(cfg.ModelCatalogURL, catalogCache, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Model catalog bootstrap failed")
	}

	apiRouter, authSessions, turnSvc, err := backend.New(sqlDB, logger, cfg.MasterKey, cfg.ToolLimits, catalog)
	if err != nil {
		logger.Fatal().Err(err).Msg("Vault bootstrap failed")
	}
	authMiddleware, err := auth.NewMiddleware(cfg.Token, authSessions)
	if err != nil {
		logger.Fatal().Err(err).Msg("Auth bootstrap failed")
	}
	apiHandler := authMiddleware.Authenticate(apiRouter)

	gw, err := gateway.New(&gateway.Config{
		ResolveModel:         turnSvc.ResolveModel,
		Journal:              turnSvc,
		GetAgentConfig:       turnSvc.GetAgentConfig,
		Tools:                turnSvc.AgentTools,
		GetAgentThread:       turnSvc.GetAgentThread,
		NoteDiscoveredWindow: turnSvc.RecordDiscoveredWindow,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create gateway handler")
	}

	// Component mounts: the API under /api, prefix stripped on the way
	// in; gateway at /ws behind the same session guard (the upgrade
	// carries the cookie); /healthz comes from utils.Serve.
	root := http.NewServeMux()
	root.Handle("/api/", http.StripPrefix("/api", apiHandler))
	root.Handle("GET /ws", authMiddleware.Guard(gw))

	if err := utils.Serve(ctx, root, cfg.Addr, logger); err != nil {
		logger.Fatal().Err(err).Msg("Server failed")
	}
	// Retire the signal handler now that Serve returned: a second
	// Ctrl-C during the drain below otherwise lands in a channel
	// nobody reads.
	cancel()

	// The server closed its connections; give running turns a
	// bounded moment to settle their partial answers into the
	// thread before the process exits.
	gw.Drain(10 * time.Second)

	// Close the tool layer: live MCP clients are torn down and the audit
	// retention ticker stops.
	turnSvc.CloseTools()
}

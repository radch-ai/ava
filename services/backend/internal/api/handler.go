package api

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/services/backend/internal/api/agent"
	"github.com/radch-ai/ava/services/backend/internal/api/user"
)

func New(dbPool *pgxpool.Pool, logger zerolog.Logger) http.Handler {
	userStore := user.NewPGStore(dbPool)
	userService := user.NewService(userStore)
	userHandler := user.NewHandler(userService, logger)

	agentStore := agent.NewPGStore(dbPool)
	agentService := agent.NewService(agentStore)
	agentHandler := agent.NewHandler(agentService, logger)

	mux := http.NewServeMux()
	mux.Handle("/user/agents", http.HandlerFunc(userHandler.GetUserAgents))
	mux.Handle("/user/thread", http.HandlerFunc(userHandler.GetThread))
	mux.Handle("/user/agent/", http.HandlerFunc(agentHandler.AgentConfigurationHandler))

	return mux
}

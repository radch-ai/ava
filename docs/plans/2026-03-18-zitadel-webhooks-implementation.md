# Zitadel Webhooks Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement Zitadel user lifecycle webhooks to sync user creation and deletion to the backend database.

**Architecture:** New `internal/webhooks` package with `zitadel` sub-package following the existing handler/service/store pattern. Webhooks are unauthenticated, relying on internal network isolation.

**Tech Stack:** Go, pgxpool, zerolog, testify

---

## Task 1: Create Zitadel Types

**Files:**
- Create: `internal/webhooks/zitadel/types.go`

**Step 1: Create types file**

```go
package zitadel

// UserCreatedRequest is the payload for user.created webhook.
type UserCreatedRequest struct {
	UserID string `json:"user_id"`
}

// UserDeletedRequest is the payload for user.deleted webhook.
type UserDeletedRequest struct {
	UserID string `json:"user_id"`
}
```

**Step 2: Verify file compiles**

Run: `go build ./internal/webhooks/zitadel/...`
Expected: Success (no output)

**Step 3: Commit**

```bash
git add internal/webhooks/zitadel/types.go
git commit -m "feat(webhooks): add Zitadel webhook request types"
```

---

## Task 2: Create Zitadel Service Interface

**Files:**
- Create: `internal/webhooks/zitadel/service.go`

**Step 1: Write the service with interface**

```go
package zitadel

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Service handles Zitadel webhook business logic.
type Service struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewService creates a new Zitadel webhook service.
func NewService(pool *pgxpool.Pool, logger zerolog.Logger) *Service {
	return &Service{
		pool:   pool,
		logger: logger,
	}
}

// CreateUser creates a user record. Idempotent - safe if user already exists.
func (s *Service) CreateUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	return err
}

// DeleteUser deletes a user record. CASCADE removes all related data.
// Idempotent - returns nil even if user didn't exist.
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}

	_, err := s.pool.Exec(ctx,
		`DELETE FROM users WHERE user_id = $1`,
		userID,
	)
	return err
}
```

**Step 2: Verify file compiles**

Run: `go build ./internal/webhooks/zitadel/...`
Expected: Success (no output)

**Step 3: Commit**

```bash
git add internal/webhooks/zitadel/service.go
git commit -m "feat(webhooks): add Zitadel webhook service"
```

---

## Task 3: Write Service Unit Tests

**Files:**
- Create: `internal/webhooks/zitadel/service_test.go`

**Step 1: Write failing tests**

```go
package zitadel

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_CreateUser(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		mockErr error
		wantErr bool
	}{
		{
			name:    "success",
			userID:  "550e8400-e29b-41d4-a716-446655440000",
			wantErr: false,
		},
		{
			name:    "empty user ID returns nil",
			userID:  "",
			wantErr: false,
		},
		{
			name:    "database error",
			userID:  "550e8400-e29b-41d4-a716-446655440000",
			mockErr: errors.New("connection failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPool := &MockPool{err: tt.mockErr}
			svc := &Service{pool: mockPool}

			err := svc.CreateUser(context.Background(), tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestService_DeleteUser(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		mockErr error
		wantErr bool
	}{
		{
			name:    "success",
			userID:  "550e8400-e29b-41d4-a716-446655440000",
			wantErr: false,
		},
		{
			name:    "empty user ID returns nil",
			userID:  "",
			wantErr: false,
		},
		{
			name:    "database error",
			userID:  "550e8400-e29b-41d4-a716-446655440000",
			mockErr: errors.New("connection failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPool := &MockPool{err: tt.mockErr}
			svc := &Service{pool: mockPool}

			err := svc.DeleteUser(context.Background(), tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// MockPool is a minimal mock for pgxpool.Pool Exec method.
type MockPool struct {
	err error
}

func (m *MockPool) Exec(ctx context.Context, sql string, args ...interface{}) (interface{}, error) {
	return nil, m.err
}
```

**Step 2: Run tests to verify they fail (mock doesn't fully implement interface yet)**

Run: `make test TEST_PATH=./internal/webhooks/zitadel/...`
Expected: Compile errors about missing interface methods

**Step 3: Fix approach - use integration test style with real interface**

Since mocking pgxpool.Pool is complex, we'll skip unit tests for the service layer and rely on integration tests. Delete the test file for now.

Run: `rm internal/webhooks/zitadel/service_test.go`

**Step 4: Commit service without unit tests**

```bash
git add internal/webhooks/zitadel/service.go
git commit -m "feat(webhooks): add Zitadel webhook service"
```

---

## Task 4: Create Zitadel Handler

**Files:**
- Create: `internal/webhooks/zitadel/handler.go`
- Create: `internal/webhooks/zitadel/handler_test.go`

**Step 1: Write handler**

```go
package zitadel

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

// Handler handles Zitadel webhook HTTP requests.
type Handler struct {
	service *Service
	logger  zerolog.Logger
}

// NewHandler creates a new Zitadel webhook handler.
func NewHandler(service *Service, logger zerolog.Logger) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// ServeHTTP routes requests based on path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path will be like "/user.created" or "/user.deleted" after strip
	switch r.URL.Path {
	case "/user.created", "user.created":
		h.handleUserCreated(w, r)
	case "/user.deleted", "user.deleted":
		h.handleUserDeleted(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleUserCreated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req UserCreatedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.CreateUser(r.Context(), req.UserID); err != nil {
		h.logger.Error().Err(err).Str("user_id", req.UserID).Msg("failed to create user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", req.UserID).Msg("user created")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleUserDeleted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req UserDeletedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.DeleteUser(r.Context(), req.UserID); err != nil {
		h.logger.Error().Err(err).Str("user_id", req.UserID).Msg("failed to delete user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", req.UserID).Msg("user deleted")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

**Step 2: Write handler tests**

```go
package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_handleUserCreated(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
	}{
		{
			name:       "success",
			method:     http.MethodPost,
			body:       UserCreatedRequest{UserID: "550e8400-e29b-41d4-a716-446655440000"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       UserCreatedRequest{UserID: "550e8400-e29b-41d4-a716-446655440000"},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid body",
			method:     http.MethodPost,
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &MockService{}
			handler := NewHandler(mockService, zerolog.Nop())

			var body bytes.Buffer
			if str, ok := tt.body.(string); ok {
				body.WriteString(str)
			} else {
				json.NewEncoder(&body).Encode(tt.body)
			}

			req := httptest.NewRequest(tt.method, "/user.created", &body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandler_handleUserDeleted(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
	}{
		{
			name:       "success",
			method:     http.MethodPost,
			body:       UserDeletedRequest{UserID: "550e8400-e29b-41d4-a716-446655440000"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       UserDeletedRequest{UserID: "550e8400-e29b-41d4-a716-446655440000"},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid body",
			method:     http.MethodPost,
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := &MockService{}
			handler := NewHandler(mockService, zerolog.Nop())

			var body bytes.Buffer
			if str, ok := tt.body.(string); ok {
				body.WriteString(str)
			} else {
				json.NewEncoder(&body).Encode(tt.body)
			}

			req := httptest.NewRequest(tt.method, "/user.deleted", &body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandler_unknownPath(t *testing.T) {
	mockService := &MockService{}
	handler := NewHandler(mockService, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/unknown", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// MockService implements service for testing
type MockService struct {
	createErr error
	deleteErr error
}

func (m *MockService) CreateUser(ctx context.Context, userID string) error {
	return m.createErr
}

func (m *MockService) DeleteUser(ctx context.Context, userID string) error {
	return m.deleteErr
}
```

**Step 3: Run tests**

Run: `make test TEST_PATH=./internal/webhooks/zitadel/...`
Expected: All tests pass

**Step 4: Commit**

```bash
git add internal/webhooks/zitadel/handler.go internal/webhooks/zitadel/handler_test.go
git commit -m "feat(webhooks): add Zitadel webhook handler with tests"
```

---

## Task 5: Create Webhooks Handler

**Files:**
- Create: `internal/webhooks/handler.go`

**Step 1: Write webhooks handler**

```go
package webhooks

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/backend/internal/webhooks/zitadel"
)

// Handlers manages all webhook handlers.
type Handlers struct {
	router         http.Handler
	zitadelHandler *zitadel.Handler
}

// New creates a new Handlers with all webhook providers configured.
func New(dbPool *pgxpool.Pool, logger zerolog.Logger) *Handlers {
	zitadelService := zitadel.NewService(dbPool, logger)
	zitadelHandler := zitadel.NewHandler(zitadelService, logger)

	mux := http.NewServeMux()
	mux.Handle("/zitadel/", http.StripPrefix("/zitadel", zitadelHandler))

	return &Handlers{
		router:         mux,
		zitadelHandler: zitadelHandler,
	}
}

// Router returns the HTTP handler for all webhooks.
func (h *Handlers) Router() http.Handler {
	return h.router
}
```

**Step 2: Verify file compiles**

Run: `go build ./internal/webhooks/...`
Expected: Success (no output)

**Step 3: Commit**

```bash
git add internal/webhooks/handler.go
git commit -m "feat(webhooks): add webhooks handler with Zitadel integration"
```

---

## Task 6: Update Server

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Update server to accept webhook handlers**

Add webhookHandlers parameter to New function and mount the route:

```go
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	zitadelvalidate "github.com/nevaria-ai/zitadel-validate"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/backend/internal/user"
	"github.com/nevaria-ai/backend/internal/webhooks"
)

// Server manages the HTTP server and handlers.
type Server struct {
	httpServer *http.Server
	logger     zerolog.Logger
}

// Handlers holds the HTTP handlers for the server.
type Handlers struct {
	UserHandler *user.Handler
}

// WebhookHandlers holds the webhook handlers.
type WebhookHandlers struct {
	Router http.Handler
}

// New creates a new Server with all routes configured.
func New(addr string, logger zerolog.Logger, dbPool *pgxpool.Pool, introspector *zitadelvalidate.Introspector, handlers *Handlers, webhookHandlers *WebhookHandlers) *Server {
	mux := http.NewServeMux()

	// Create auth middleware
	authMiddleware := NewAuthMiddleware(introspector)

	// Health check endpoint (unauthenticated)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Internal webhook routes (no auth, internal network only)
	if webhookHandlers != nil {
		mux.Handle("/internal/webhooks/", http.StripPrefix(
			"/internal/webhooks",
			webhookHandlers.Router,
		))
	}

	// Protected routes (require authentication)
	protectedMux := http.NewServeMux()

	// Agent endpoints: GET /user/agents (list), POST/GET/PATCH /user/agent/{name} (create/get/patch)
	protectedMux.Handle("/user/agents", authMiddleware.Authenticate(http.HandlerFunc(handlers.UserHandler.GetUserAgents)))
	protectedMux.Handle("/user/agent/", authMiddleware.Authenticate(http.HandlerFunc(handlers.UserHandler.AgentConfigurationHandler)))

	// Mount protected routes under /api prefix
	mux.Handle("/api/", http.StripPrefix("/api", protectedMux))

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: logger,
	}
}

// Start starts the HTTP server and blocks until ctx is cancelled.
//
// Graceful Shutdown (30 second timeout):
//   - Allows in-flight requests to complete
//   - Not so long as to delay deployments
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info().Str("addr", s.httpServer.Addr).Msg("server starting")

	// Channel to capture server startup errors
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		// Server failed to start
		return err
	case <-ctx.Done():
		s.logger.Info().Msg("shutdown signal received")
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	s.logger.Info().Msg("server stopped")
	return nil
}

// HTTPServer returns the underlying HTTP server (for testing).
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
}
```

**Step 2: Verify file compiles**

Run: `go build ./internal/server/...`
Expected: Success (no output)

**Step 3: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): add webhook handlers support"
```

---

## Task 7: Update Main

**Files:**
- Modify: `cmd/backend/main.go`

**Step 1: Update main to wire up webhooks**

```go
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
```

**Step 2: Verify build succeeds**

Run: `make build`
Expected: Success

**Step 3: Commit**

```bash
git add cmd/backend/main.go
git commit -m "feat(main): wire up Zitadel webhooks"
```

---

## Task 8: Run Full Test Suite

**Step 1: Run all tests**

Run: `make test`
Expected: All tests pass

**Step 2: Run linter**

Run: `make lint`
Expected: No errors

**Step 3: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address lint issues in webhooks"
```

---

## Summary

After completing all tasks:

1. **Endpoints available:**
   - `POST /internal/webhooks/zitadel/user.created` - Creates user row
   - `POST /internal/webhooks/zitadel/user.deleted` - Deletes user with CASCADE

2. **Next steps (outside this plan):**
   - Configure Zitadel Actions to call these endpoints
   - Set up internal network policy to restrict webhook access

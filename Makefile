BINARY_NAME ?= backend

.PHONY: build run test test-unit test-integration
.PHONY: clean deps fmt lint help
.PHONY: migrate sqlc-generate migrate-up migrate-down migrate-version
.PHONY: install-tools

## build: Build the application binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	go build -mod=vendor -o bin/$(BINARY_NAME) ./cmd/backend

## run: Run the application
run:
	@echo "Running $(BINARY_NAME)..."
	go run -mod=vendor ./cmd/backend/main.go

## test: Run unit tests with race and coverage
test:
	@if [ -z "$(TEST_PATH)" ]; then \
		echo "Running unit tests for all packages..."; \
		CGO_ENABLED=1 go test -mod=vendor -race -coverprofile=coverage.out -v ./...; \
	else \
		echo "Running unit tests for $(TEST_PATH)..."; \
		CGO_ENABLED=1 go test -mod=vendor -race -coverprofile=coverage.out -v $(TEST_PATH); \
	fi
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## test-unit: Run unit tests only (same as test, for compatibility)
test-unit:
	@echo "Running unit tests..."
	$(MAKE) test

## test-integration: Run integration tests (uses testcontainers, no external DB needed)
test-integration:
	@echo "Running integration tests..."
	CGO_ENABLED=1 go test -mod=vendor -race -v -tags integration ./... -timeout 20m

## migrate: Run database migrations
migrate:
	@echo "Running database migrations..."
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required"; \
		exit 1; \
	fi
	go run -mod=vendor ./cmd/backend/main.go --migrate

## sqlc-generate: Generate Go code from SQL queries
sqlc-generate:
	@echo "Generating sqlc code..."
	sqlc generate

## install-tools: Install development tools (sqlc, golangci-lint)
install-tools:
	@echo "Installing development tools to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	GOBIN=~/.local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ~/.local/bin; \
	fi
	@echo "Tools installed. Ensure ~/.local/bin is in your PATH."

## migrate-up: Run database migrations up
migrate-up:
	@echo "Running migrations up..."
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required"; \
		exit 1; \
	fi
	migrate -database $(DATABASE_URL) -path schema/migrations up

## migrate-down: Rollback database migrations
migrate-down:
	@echo "Rolling back migrations..."
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required"; \
		exit 1; \
	fi
	migrate -database $(DATABASE_URL) -path schema/migrations down

## migrate-version: Show current migration version
migrate-version:
	@echo "Checking migration version..."
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required"; \
		exit 1; \
	fi
	migrate -database $(DATABASE_URL) -path schema/migrations version

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin
	@rm -f coverage.out coverage.html coverage.integration.out coverage.integration.html
	@rm -f coverage.e2e.out coverage.e2e.html

## deps: Update vendor directory
deps:
	@echo "Updating vendor directory..."
	go mod tidy
	go mod vendor

## update-zitadel-validate: Update private zitadel-validate dependency
update-zitadel-validate:
	@echo "Updating github.com/nevaria-ai/zitadel-validate..."
	GOPRIVATE=github.com/nevaria-ai/* \
	GIT_CONFIG_COUNT=1 \
	GIT_CONFIG_KEY_0=url.git@github.com:.insteadOf \
	GIT_CONFIG_VALUE_0=https://github.com/ \
		go get github.com/nevaria-ai/zitadel-validate@main
	go mod tidy
	go mod vendor

## fmt: Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

## lint: Run linter with auto-fix
lint:
	@echo "Running linter with fixes..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
	else \
		echo "golangci-lint not installed."; \
	fi

## help: Show this help message
help:
	@echo "Usage: make [target] [TEST_PATH=...] [DATABASE_URL=...]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

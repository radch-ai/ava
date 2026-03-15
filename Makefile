BINARY_NAME ?= backend

.PHONY: build run test test-unit test-integration test-e2e clean deps fmt lint help
.PHONY: docker-test-logs docker-build-test migrate sqlc-generate migrate-up migrate-down migrate-version

## build: Build the application binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	go build -o bin/$(BINARY_NAME) ./cmd/backend

## run: Run the application
run:
	@echo "Running $(BINARY_NAME)..."
	go run ./cmd/backend/main.go

## test: Run unit tests with race and coverage
test:
	@if [ -z "$(TEST_PATH)" ]; then \
		echo "Running unit tests for all packages..."; \
		CGO_ENABLED=1 go test -race -coverprofile=coverage.out -v ./...; \
	else \
		echo "Running unit tests for $(TEST_PATH)..."; \
		CGO_ENABLED=1 go test -race -coverprofile=coverage.out -v $(TEST_PATH); \
	fi
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	CGO_ENABLED=1 go test -race -v ./tests/unit/...

## test-integration: Run integration tests with Docker compose (auto cleanup)
## Usage (optional): make test-integration TEST_PATH=./tests/integration
test-integration:
	@echo "Starting Docker test services..."
	@docker compose -f tests/docker-compose.test.yml up -d --wait --quiet-pull
	@sleep 2
	@if [ -z "$(TEST_PATH)" ]; then \
		echo "Running integration tests for all packages..."; \
		TEST_DATABASE_URL="postgres://test:test@localhost:5433/test?sslmode=disable" \
			go test -race -coverprofile=coverage.integration.out -tags=integration -v ./... -timeout 5m; \
	else \
		echo "Running integration tests for $(TEST_PATH)..."; \
		TEST_DATABASE_URL="postgres://test:test@localhost:5433/test?sslmode=disable" \
			go test -race -coverprofile=coverage.integration.out -tags=integration -v $(TEST_PATH) -timeout 5m; \
	fi
	@go tool cover -html=coverage.integration.out -o coverage.integration.html
	@echo "Integration coverage report: coverage.integration.html"
	@echo "Stopping Docker test services..."
	@docker compose -f tests/docker-compose.test.yml down -v

## test-e2e: Run E2E tests with Docker compose (auto cleanup)
test-e2e:
	@echo "Starting Docker test services..."
	@docker compose -f tests/docker-compose.test.yml up -d --wait --quiet-pull
	@sleep 2
	go test -race -coverprofile=coverage.e2e.out -tags=e2e -v ./tests/e2e/... -timeout 10m
	@go tool cover -html=coverage.e2e.out -o coverage.e2e.html
	@echo "E2E coverage report: coverage.e2e.html"
	@echo "Stopping Docker test services..."
	@docker compose -f tests/docker-compose.test.yml down -v

## migrate: Run database migrations
migrate:
	@echo "Running database migrations..."
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "Error: DATABASE_URL is required"; \
		exit 1; \
	fi
	go run ./cmd/backend/main.go --migrate

## sqlc-generate: Generate Go code from SQL queries
sqlc-generate:
	@echo "Generating sqlc code..."
	sqlc generate

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

## docker-test-logs: Show logs from Docker test services
docker-test-logs:
	@docker compose -f tests/docker-compose.test.yml logs --follow

## docker-build-test: Build Docker image to verify Dockerfile
docker-build-test:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):latest .

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin
	@rm -f coverage.out coverage.html coverage.integration.out coverage.integration.html
	@rm -f coverage.e2e.out coverage.e2e.html

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

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

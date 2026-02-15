include .makedefs

.PHONY: all build run test clean generate docker-build docker-run install-hooks help
.PHONY: test-unit test-all test-integration test-e2e docker-test-up docker-test-down docker-test-logs
.PHONY: test-cover deps fmt lint lint-fix

all: build

BUILD_DIR = bin

## help: Show this help message
help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ''
	@echo 'Testing:'
	@echo '  test                         Run all tests'
	@echo '  test-unit                    Run unit tests only'
	@echo '  test-all                     Run all test suites (unit + integration)'
	@echo '  test-integration             Run integration tests with docker services'
	@echo '  test-integration-with-docker Start Docker, run integration tests, cleanup'
	@echo '  test-e2e                     Run E2E tests'
	@echo '  docker-test-up               Start Docker services for testing'
	@echo '  docker-test-down             Stop Docker test services'
	@echo '  docker-test-logs             Show Docker test service logs'

## build: Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gateway

## run: Run the server locally
run:
	go run ./cmd/gateway/main.go

## test: Run all tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## test-cover: Run tests with coverage report
test-cover: test

## test-unit: Run unit tests only
test-unit:
	@echo "Running unit tests..."
	go test -v ./tests/unit/...

## test-all: Run all test suites (unit + integration)
test-all: test-unit test-integration

## test-integration: Run integration tests with docker services
test-integration:
	@echo "Running integration tests..."
	@if [ -z "$$TEST_DATABASE_URL" ]; then \
		echo "Error: TEST_DATABASE_URL not set"; \
		echo "Set it with: export TEST_DATABASE_URL=\"postgres://postgres:postgres@localhost:5433/nevaria_gateway_test?sslmode=disable\""; \
		echo "Or use: make test-integration-with-docker"; \
		exit 1; \
	fi
	go test -tags=integration -v ./tests/integration/... -timeout 5m

## test-integration-with-docker: Start Docker, run integration tests, cleanup
test-integration-with-docker:
	@echo "Starting Docker test services..."
	@docker compose -f tests/docker-compose.test.yml up -d --wait --quiet-pull
	@sleep 2
	@echo "Running integration tests..."
	@TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5433/nevaria_gateway_test?sslmode=disable" \
		go test -tags=integration -count=1 -v ./tests/integration/... -timeout 5m
	@echo "Stopping Docker test services..."
	@docker compose -f tests/docker-compose.test.yml down -v

## test-e2e: Run E2E tests
test-e2e:
	@echo "Starting Docker test services..."
	@docker compose -f tests/docker-compose.test.yml up -d --wait --quiet-pull
	@echo "Running E2E tests..."
	@go test -tags=e2e -v ./tests/e2e/... -timeout 10m
	@echo "Stopping Docker test services..."
	@docker compose -f tests/docker-compose.test.yml down -v

## docker-test-up: Start Docker services for testing
docker-test-up:
	@echo "Starting Docker test services..."
	@docker compose -f tests/docker-compose.test.yml up -d --wait
	@echo "Services started. Use 'make docker-test-logs' to view logs."

## docker-test-down: Stop Docker test services
docker-test-down:
	@echo "Stopping Docker test services..."
	@docker compose -f tests/docker-compose.test.yml down -v

## docker-test-logs: Show Docker test service logs
docker-test-logs:
	@docker compose -f tests/docker-compose.test.yml logs --follow

## clean: Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

## generate: Generate code from project details
generate:
	go run scripts/generate-project-details.go

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

## fmt: Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

## lint: Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Run: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin"; \
	fi

## lint-fix: Run linter with auto-fix
lint-fix:
	@echo "Running linter with fixes..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
	else \
		echo "golangci-lint not installed."; \
	fi

## docker-build: Build Docker image
docker-build:
	docker build -t gateway:latest .

## docker-run: Run Docker container
docker-run:
	docker run -p 8080:8080 gateway:latest

## install-hooks: Install git hooks
install-hooks:
	@echo "Installing pre-commit hook..."
	@ln -sf ../../scripts/hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed."

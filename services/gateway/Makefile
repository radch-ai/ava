# Gateway-specific test and docker-compose targets.
# For build, run, test, validate, docker, and clean, use the repo-root justfile:
#   just --list   (from repo root)

.PHONY: help test-integration test-e2e docker-test-logs docker-build-test

## help: Show this help message
help:
	@echo 'Usage: make [target] [TEST_PATH=...]'
	@echo ''
	@echo 'Repo-root equivalents (run from repo root):'
	@echo '  just build gateway          Build binary'
	@echo '  just run gateway            Run server'
	@echo '  just test gateway           Unit tests with coverage'
	@echo '  just validate               Format + lint'
	@echo '  just docker gateway         Build Docker image'
	@echo '  just clean                  Remove bin/ and coverage artifacts'
	@echo ''
	@echo 'Gateway-specific targets (run from services/gateway):'
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

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

## docker-test-logs: Show logs from Docker test services
docker-test-logs:
	@docker compose -f tests/docker-compose.test.yml logs --follow

## docker-build-test: Build Docker image to verify Dockerfile
docker-build-test:
	@echo "Building Docker image..."
	docker build -t gateway:latest .

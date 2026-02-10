.PHONY: test build run clean generate docker-build docker-run install-hooks help

# Include generated project details
-include .makedefs

BUILD_DIR = bin

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

build: ## Build the binary
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gateway

run: ## Run the server locally
	go run ./cmd/gateway/main.go

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

generate: ## Generate code from project details
	go run scripts/generate-project-details.go

docker-build: ## Build Docker image
	docker build -t gateway:latest .

docker-run: ## Run Docker container
	docker run -p 8080:8080 gateway:latest

install-hooks: ## Install git hooks
	@echo "Installing pre-commit hook..."
	@ln -sf ../../scripts/hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed."

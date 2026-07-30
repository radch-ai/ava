set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

allowed_services := "backend"
sqlc_services := "backend"

# Validate that a service name is in allowed_services. Used by service-arg recipes.
[private]
validate_service name:
    @test "{{ name }}" != "" || { echo "Error: service name required. Allowed: {{ allowed_services }}"; exit 1; }
    @echo "{{ allowed_services }}" | grep -qw "{{ name }}" || { echo "Error: unknown service '{{ name }}'. Allowed: {{ allowed_services }}"; exit 1; }

# Fail with a friendly pointer to install-tools when a required CLI is missing.
[private]
require_tool tool:
    @command -v "{{ tool }}" >/dev/null 2>&1 || { echo "Error: '{{ tool }}' not found on PATH. Run 'just install-tools' first."; exit 1; }

# Ensure a service's sqlc-generated db layer is up to date. Used as a build/test prerequisite.
[private]
sqlc-for service:
    @just require_tool sqlc
    @echo "-> sqlc generate: {{ service }}"
    @cd "services/{{ service }}" && sqlc generate

## --- tools ---

# Install dev tools (sqlc, golang-migrate, golangci-lint) to ~/.local/bin.
install-tools:
    @mkdir -p ~/.local/bin
    GOBIN=~/.local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
    GOBIN=~/.local/bin go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
    @if ! command -v golangci-lint >/dev/null 2>&1; then \
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ~/.local/bin; \
    fi
    @echo "Tools installed. Ensure ~/.local/bin is in your PATH."

## --- build / run ---

# Build a service into ./bin. Usage: just build <service>
build service:
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    @mkdir -p bin
    go build -o bin/{{ service }} ./services/{{ service }}

# Run a service. Usage: just run <service>
run service:
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    go run ./services/{{ service }}

# Build a service Docker image (tag: ava-<service>:latest). Usage: just docker <service>
docker service:
    @just validate_service {{ service }}
    docker build -t ava-{{ service }}:latest -f go-services.Dockerfile --build-arg SERVICE={{ service }} .

## --- test ---

# Run unit tests with race + coverage for a service.
# Usage: just test <service> [test_path=./some/pkg/...]
test service test_path="":
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    #!/usr/bin/env bash
    path="{{ test_path }}"
    CGO_ENABLED=1 go test -race -coverprofile=coverage.{{ service }}.out -v ${path:-./services/{{ service }}/...}
    go tool cover -html=coverage.{{ service }}.out -o coverage.{{ service }}.html
    @echo "Coverage report: coverage.{{ service }}.html"

# Run integration tests for a service (uses testcontainers, no external DB needed).
# Usage: just test-integration <service>
test-integration service:
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    CGO_ENABLED=1 go test -race -v -tags integration ./services/{{ service }}/... -timeout 20m

## --- migrate / codegen ---

# Run migrations via golang-migrate CLI. Usage: just migrate-cli <service> <up|down|version>. Requires DATABASE_URL.
migrate-cli service direction:
    @just validate_service {{ service }}
    @just require_tool migrate
    @test "{{ direction }}" = "up" -o "{{ direction }}" = "down" -o "{{ direction }}" = "version" || { echo "Error: direction must be one of: up down version (got: {{ direction }})"; exit 1; }
    @test -n "${DATABASE_URL:-}" || { echo "Error: DATABASE_URL is required"; exit 1; }
    migrate -database "$DATABASE_URL" -path services/{{ service }}/schema/migrations {{ direction }}

# Run sqlc generate for every service that uses it (see sqlc_services).
sqlc-generate:
    @for svc in {{ sqlc_services }}; do \
        just sqlc-for "$$svc" || exit 1; \
    done

## --- quality ---

# Run golangci-lint with --fix across all Go code.
lint:
    @just require_tool golangci-lint
    @golangci-lint run --fix ./...

## --- clean ---

# Remove root bin/ and coverage artifacts.
clean:
    @rm -rf bin
    @rm -f coverage.*

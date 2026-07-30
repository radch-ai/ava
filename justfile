set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

allowed_services := "backend"
sqlc_services := "backend"

# Validate that a service name is in allowed_services. Used by service-arg recipes.
[private]
validate_service name:
    @test "{{ name }}" != "" || { echo "Error: service name required. Allowed: {{ allowed_services }}"; exit 1; }
    @echo "{{ allowed_services }}" | grep -qw "{{ name }}" || { echo "Error: unknown service '{{ name }}'. Allowed: {{ allowed_services }}"; exit 1; }

# Ensure a service's sqlc-generated db layer is up to date. Used as a build/test prerequisite.
[private]
sqlc-for service:
    @echo "-> sqlc generate: {{ service }}"
    @cd "services/{{ service }}" && sqlc generate

## --- tools ---

# Install dev tools (sqlc, golangci-lint) to ~/.local/bin.
install-tools:
    @mkdir -p ~/.local/bin
    GOBIN=~/.local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
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
    go build -o bin/{{ service }} ./services/{{ service }}/cmd/{{ service }}

# Run a service. Usage: just run <service>
run service:
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    go run ./services/{{ service }}/cmd/{{ service }}/main.go

## --- test ---

# Run unit tests with race + coverage for a service.
# Usage: just test <service> [test_path=./some/pkg/...]
test service test_path="":
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    #!/usr/bin/env bash
    path="{{ test_path }}"
    CGO_ENABLED=1 go test -race -coverprofile=coverage.out -v ${path:-./services/{{ service }}/...}
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report: coverage.html"

# Run integration tests for a service (uses testcontainers, no external DB needed).
# Usage: just test-integration <service>
test-integration service:
    @just validate_service {{ service }}
    @just sqlc-for {{ service }}
    CGO_ENABLED=1 go test -race -v -tags integration ./services/{{ service }}/... -timeout 20m

## --- migrate / codegen ---

# Run migrations via golang-migrate CLI. Usage: just migrate-cli <up|down|version>. Requires DATABASE_URL.
migrate-cli direction:
    #!/usr/bin/env bash
    case "{{ direction }}" in
        up|down|version) ;;
        *) echo "Error: direction must be one of: up down version (got: {{ direction }})"; exit 1 ;;
    esac
    if [ -z "${DATABASE_URL:-}" ]; then
        echo "Error: DATABASE_URL is required"; exit 1
    fi
    migrate -database "$DATABASE_URL" -path services/backend/schema/migrations {{ direction }}

# Run sqlc generate for every service that uses it (see sqlc_services).
sqlc-generate:
    @for svc in {{ sqlc_services }}; do \
        just sqlc-for "$$svc" || exit 1; \
    done

## --- quality ---

# Run golangci-lint with --fix across all Go code.
lint:
    @if command -v golangci-lint >/dev/null 2>&1; then \
        golangci-lint run --fix ./...; \
    else \
        echo "golangci-lint not installed."; \
    fi

## --- clean ---

# Remove root bin/ and coverage artifacts.
clean:
    @rm -rf bin
    @rm -f coverage.out coverage.html coverage.integration.out coverage.integration.html
    @rm -f coverage.e2e.out coverage.e2e.html

set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

allowed_services := "ava"
sqlc_services := "ava"

# Validate that a service name is in allowed_services. Used by service-arg recipes.
[private]
validate_service name:
    @test "{{ name }}" != "" || { echo "Error: service name required. Allowed: {{ allowed_services }}"; exit 1; }
    @echo "{{ allowed_services }}" | grep -qw "{{ name }}" || { echo "Error: unknown service '{{ name }}'. Allowed: {{ allowed_services }}"; exit 1; }

# Fail with a friendly pointer to install-tools when a required CLI is missing.
[private]
require_tool tool:
    @command -v "{{ tool }}" >/dev/null 2>&1 || { echo "Error: '{{ tool }}' not found on PATH. Run 'just install-tools' first."; exit 1; }

# List available recipes
default:
    @just --list

## --- tools / codegen ---

# Install dev tools (sqlc, golang-migrate, goimports, golangci-lint) to ~/.local/bin.
install-tools:
    @mkdir -p ~/.local/bin
    GOBIN=~/.local/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
    GOBIN=~/.local/bin go install -tags sqlite github.com/golang-migrate/migrate/v4/cmd/migrate@latest
    GOBIN=~/.local/bin go install golang.org/x/tools/cmd/goimports@latest
    @if ! command -v golangci-lint >/dev/null 2>&1; then \
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ~/.local/bin; \
    fi
    @echo "Tools installed. Ensure ~/.local/bin is in your PATH."

# Regenerate the sqlc db layer (backend/queries + backend/internal/schema/migrations → backend/internal/db).
# Functional once queries land; sqlc rejects empty query paths.
# Gated into validate/build/test when they do.
sqlc-generate:
    @just require_tool sqlc
    @cd backend && sqlc generate

# Run sqlc-generate, but only for services that use it (see sqlc_services).
[private]
sqlc-generate-for service:
    @case " {{ sqlc_services }} " in *" {{ service }} "*) just sqlc-generate ;; esac

## --- build / run ---

# Build a service into ./bin. Usage: just build <service>
build service:
    @just validate_service {{ service }}
    @just sqlc-generate-for {{ service }}
    @mkdir -p bin
    go build -o bin/{{ service }} ./cmd/{{ service }}

# Build a service, then run its binary from bin/. Dev runs use the in-memory
# database unless AVA_DB is set.
run service: (build service)
    AVA_DB="${AVA_DB:-:memory:}" ./bin/{{ service }}

## --- test ---

# Run unit tests with race + coverage. Usage: just test <service> [test_path]
test service test_path="":
    @just validate_service {{ service }}
    @just sqlc-generate-for {{ service }}
    #!/usr/bin/env bash
    path="{{ test_path }}"
    CGO_ENABLED=1 go test -race -coverprofile=coverage.{{ service }}.out ${path:-./...}
    go tool cover -html=coverage.{{ service }}.out -o coverage.{{ service }}.html
    @echo "Coverage report: coverage.{{ service }}.html"

## --- migrate ---

# Run migrations via golang-migrate CLI (manual rollbacks; startup runs them
# embedded). Usage: just migrate-cli <up|down|version> [steps|all] — down
# needs a step count or 'all' (e.g. just migrate-cli down 1). Database:
# sqlite://$AVA_DB.
migrate-cli direction n="":
    @just require_tool migrate
    @test "{{ direction }}" = "up" -o "{{ direction }}" = "down" -o "{{ direction }}" = "version" || { echo "Error: direction must be one of: up down version (got: {{ direction }})"; exit 1; }
    @test "{{ direction }}" != "down" -o "{{ n }}" != "" || { echo "Error: down needs a step count or 'all'. Usage: just migrate-cli down 1 | just migrate-cli down all"; exit 1; }
    @test -n "${AVA_DB:-}" || { echo "Error: AVA_DB is required"; exit 1; }
    @mkdir -p "$(dirname "$AVA_DB")"
    @migrate -database "sqlite://$AVA_DB" -path backend/internal/schema/migrations {{ direction }} {{ if n == "all" { "-all" } else { n } }}

## --- validate ---

# go mod tidy → golangci-lint --fix → go fmt → goimports.
validate:
    @just require_tool golangci-lint
    @just require_tool goimports
    go mod tidy
    golangci-lint run --fix ./...
    go fmt ./...
    goimports -w -local github.com/radch-ai/ava .

## --- clean ---

# Remove root bin/ and coverage artifacts.
clean:
    @rm -rf bin
    @rm -f coverage.*

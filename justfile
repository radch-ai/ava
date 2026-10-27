set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

allowed_services := "ava"

# Validate that a service name is in allowed_services. Used by service-arg recipes.
[private]
validate_service name:
    @test "{{ name }}" != "" || { echo "Error: service name required. Allowed: {{ allowed_services }}"; exit 1; }
    @echo "{{ allowed_services }}" | grep -qw "{{ name }}" || { echo "Error: unknown service '{{ name }}'. Allowed: {{ allowed_services }}"; exit 1; }

# List available recipes
default:
    @just --list

## --- build / run ---

# Build a service into ./bin. Usage: just build <service>
build service:
    @just validate_service {{ service }}
    @mkdir -p bin
    go build -o bin/{{ service }} ./cmd/{{ service }}

# Build a service, then run its binary from bin/. Usage: just run <service>
run service: (build service)
    ./bin/{{ service }}

## --- test ---

# Run unit tests with race + coverage. Usage: just test <service> [test_path]
test service test_path="":
    @just validate_service {{ service }}
    #!/usr/bin/env bash
    path="{{ test_path }}"
    CGO_ENABLED=1 go test -race -coverprofile=coverage.{{ service }}.out ${path:-./...}
    go tool cover -html=coverage.{{ service }}.out -o coverage.{{ service }}.html
    @echo "Coverage report: coverage.{{ service }}.html"

## --- validate ---

# Tidy, format, and vet the module.
validate:
    go mod tidy
    gofmt -l -w .
    go vet ./...

## --- clean ---

# Remove root bin/ and coverage artifacts.
clean:
    @rm -rf bin
    @rm -f coverage.*

# Project Package

Project metadata and constants.

## Purpose

The project package contains compile-time metadata about the gateway application.

## Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `Name` | `Nevaria` | Project name |
| `Slug` | `nevaria` | URL-friendly name |
| `Description` | `AI Agent Gateway` | Project description |
| `ModulePath` | `github.com/nevaria-ai/gateway` | Go module path |
| `BinaryName` | `nevaria-gateway` | Compiled binary name |

## Variables

| Variable | Type | Description |
|----------|------|-------------|
| `Version` | string | Build-time version (set via ldflags, default: "dev") |

## Build

```bash
# Development build (version: dev)
go build -o gateway ./cmd/gateway

# Production build with version
go build -ldflags "-X github.com/nevaria-ai/gateway/internal/project.Version=v1.0.0" -o gateway ./cmd/gateway
```

## File Locations

- [`internal/project/project.go`](../internal/project/project.go) - Metadata constants

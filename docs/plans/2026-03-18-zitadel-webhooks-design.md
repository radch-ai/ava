# Zitadel User Lifecycle Webhooks Design

## Overview

Set up Zitadel webhooks to sync user lifecycle events to the backend database, maintaining referential integrity for the `agents` table and future user-related tables.

## Zitadel Configuration

Two Actions required in Zitadel Console, both pointing to the same endpoint:

### 1. NotifyBackendUserCreated
- Trigger: `Post User Creation`
- Endpoint: `POST /internal/webhooks/zitadel/`
- Payload: `{ "event_type": "user.created", "user_id": "<zitadel-uuid>" }`

### 2. NotifyBackendUserDeleted
- Trigger: `Pre User Deletion`
- Endpoint: `POST /internal/webhooks/zitadel/`
- Payload: `{ "event_type": "user.deleted", "user_id": "<zitadel-uuid>" }`

## Backend Structure

```
internal/webhooks/
├── handler.go              # New(dbPool, logger) - wires all providers
├── zitadel/
│   ├── handler.go          # Single HTTP handler with event type routing
│   ├── service.go          # Upsert/delete user logic
│   └── types.go            # WebhookRequest with event_type
├── revenuecat/             # Future: subscription webhooks
    └── ...
```

## Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/internal/webhooks/zitadel/` | None (internal) | Handle all Zitadel events |

## Request/Response

### Request Payload

```json
{
  "event_type": "user.created",
  "user_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Event Types

| event_type | Action |
|------------|--------|
| `user.created` | INSERT INTO users (idempotent) |
| `user.deleted` | DELETE FROM users (CASCADE) |
| unknown | 200 OK (ignored, prevents Zitadel retries) |

### Response

- `200 OK` for all valid requests (including unknown event types)
- `400 Bad Request` for invalid JSON
- `405 Method Not Allowed` for non-POST requests
- `500 Internal Server Error` for database errors

## Security

- No authentication on webhook endpoints
- Relies on internal network isolation
- Zitadel can only reach these endpoints via private network

## Database

No schema changes needed:
- `users.user_id` already accepts UUID
- `agents.user_id` FK already has `ON DELETE CASCADE`

## Wiring

### webhooks/handler.go

```go
func New(dbPool *pgxpool.Pool, logger zerolog.Logger) *Handlers {
    zitadelService := zitadel.NewService(dbPool, logger)
    zitadelHandler := zitadel.NewHandler(zitadelService, logger)

    mux := http.NewServeMux()
    mux.Handle("/zitadel/", http.StripPrefix("/zitadel", zitadelHandler))

    return &Handlers{router: mux}
}
```

### main.go

```go
webhookHandlers := webhooks.New(dbPool, logger)
server.New(addr, logger, dbPool, introspector, userHandler, webhookHandlers)
```

## Flow Diagrams

### User Creation

```
Frontend signup → Zitadel creates user → Action fires
    → POST /internal/webhooks/zitadel/
    → { "event_type": "user.created", "user_id": "..." }
    → INSERT INTO users (user_id) VALUES (...)
    → User can now create agents
```

### User Deletion

```
Delete request → Zitadel Action fires (pre-deletion)
    → POST /internal/webhooks/zitadel/
    → { "event_type": "user.deleted", "user_id": "..." }
    → DELETE FROM users WHERE user_id = ...
    → CASCADE: deletes agents, configs, tools
    → Zitadel completes user deletion
```

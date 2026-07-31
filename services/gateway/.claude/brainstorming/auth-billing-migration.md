# Gateway Authentication & Billing Migration Design

**Date:** 2025-02-23
**Status:** Wave 1 Complete, Wave 2-5 Remaining

## Implementation Status

| Wave | Status | Notes |
|------|--------|-------|
| 1: Foundation & Database | ✅ Complete | Migrations working, tests passing |
| 2: Zitadel Authentication | ❌ Not Started | Currently using auth service client |
| 3: RevenueCat Webhooks | ⚠️ Partial | Webhook handler done, quota manager is stub |
| 4: Rate Limiting & Quotas | ⏳ TODO | Only stub implementation exists |
| 5: Integration & Rollout | ⏳ TODO | Feature flags, monitoring pending |

## Current Implementation

### What Works Now

**Wave 1 - Database Layer ✅**
- File: `internal/billing/migrations.go`
- Database migrations for users, usage_logs, processed_webhooks
- Idempotent migrations with transaction support
- Tests: `internal/billing/migrations_test.go` (10/10 passing)

**Wave 3 (Partial) - Webhook Handler ✅**
- File: `internal/billing/webhook_impl.go`
- HMAC signature verification
- Event handlers: INITIAL_PURCHASE, RENEWAL, CANCELLATION, REFUND, PRODUCT_CHANGE
- IP whitelist support
- Rate limiting (60 req/min)
- Idempotency via processed_webhooks table

**Auth Service Client (Current) ✅**
- File: `internal/auth/client.go`
- HTTP-based session validation
- Delegates to external auth service
- NOT Zitadel JWKS (that's Wave 2)

### What's Still Stub/TODO

**Wave 3 (Remaining) - Quota Manager ⏳**
- File: `internal/billing/quota.go`
- Stub with TODO comments
- Functions: CheckQuota(), ReserveCredit(), DeductCredit()
- Planned: 6-hour period limits, Redis caching, tier limits

**Wave 2 - Zitadel Integration ❌**
- NOT implemented yet
- Files to create: jwks.go, validator.go, middleware.go
- Planned: Direct JWKS validation, bearer JWT tokens

## Remaining Work

### Wave 2: Zitadel Authentication

**Planned Changes:**

| Current | Planned |
|---------|---------|
| HTTP session validation (`client.go`) | Zitadel JWKS validation |
| Session cookie | JWT bearer token |
| External auth service dependency | Direct Zitadel integration |

**Files to Create:**
| File | Purpose |
|------|---------|
| `internal/auth/jwks.go` | JWKS fetching and caching |
| `internal/auth/validator.go` | JWT signature and claims validation |
| `internal/auth/middleware.go` | Gin/JWT middleware integration |

**Files to Remove:**
| File | Reason |
|------|--------|
| `internal/auth/client.go` | Replaced by JWKS validation |

### Wave 3: Complete Quota Manager

**Current State:** `internal/billing/quota.go` is a stub with TODO comments.

**What Needs Implementation:**

#### Period Calculation & Credit Management

The 6-hourly declining bucket algorithm:

1. **On period change (every 6 hours):**
   - Calculate: `remaining_credits / remaining_periods * 1.1`
   - Cap at `remaining_credits` (buffer doesn't exceed available)
   - Update Redis cache

2. **On each request:**
   - Check if `period_used < period_limit`
   - If yes, reserve 1 credit
   - After LLM call, deduct actual cost from Postgres
   - If error, refund reservation

#### Redis Cache Structure to Implement

| Key Pattern | Type | TTL | Purpose |
|-------------|------|-----|---------|
| `user:{id}:period_limit` | int | 6h | Current 6-hour allowance |
| `user:{id}:period_used` | int | 6h | Credits used this period |
| `user:{id}:period_expires` | timestamp | 6h | Period end time |
| `user:{id}:tier` | string | 1h | Cached tier |
| `user:{id}:credits` | int | 5m | Cached balance |

#### Tier Limits to Enforce

| Tier | Agent Limit | Cron Limit |
|------|-------------|------------|
| free | 1 | 0 |
| pro | 5 | 10 |
| enterprise | unlimited | unlimited |

### Wave 4: Integration

- Wire up quota manager to LLM proxy handler
- Wire up quota manager to agent handler
- Add pre-request quota checks
- Add post-request credit deductions

### Wave 5: Rollout

- Feature flags
- Monitoring and metrics
- Migration guide
- Cleanup of old code

## Historical Design Context

The following sections contain the original design decisions and rationale.

### Authentication: Zitadel Direct Integration (Planned)

**Decision:** Users authenticate directly with Zitadel, gateway validates JWTs locally using JWKS.

```
Client ──OIDC+PKCE──▶ Zitadel ──JWT──▶ Gateway (JWKS validation)
```

**Status:** NOT YET IMPLEMENTED - Currently using auth service client

**Rationale:**
- Removes external auth service dependency
- Faster validation (local JWKS vs HTTP call)
- Better scalability (no auth service bottleneck)
- Standard JWT patterns

### Token Format: Bearer JWT (Planned)

**Decision:** Clients send `Authorization: Bearer <jwt>` header.

**Status:** NOT YET IMPLEMENTED - Currently using session cookies

**Rationale:**
- Standard OAuth2/OIDC pattern
- No server-side session state
- Easier mobile/client integration
- Zitadel emits JWTs natively

### Billing: RevenueCat Direct Integration ✅

**Decision:** Gateway receives RevenueCat webhooks, validates them, and manages credits/tiers.

**Status:** PARTIALLY IMPLEMENTED - Webhook handler complete, quota manager stub

**Rationale:**
- RevenueCat handles subscription logic
- Gateway owns credit enforcement
- Webhooks provide real-time updates
- Idempotency prevents double-charging

### Data Storage: Postgres ✅

**Decision:** Postgres for user data and usage logs.

**Status:** IMPLEMENTED

**Rationale:**
- ACID guarantees for credit transactions
- Rich indexing options
- Can add TimescaleDB for analytics
- Standard SQL skills

### Webhook Validation ✅

**Multi-layer security implemented:**
1. HMAC signature validation (RevenueCat shared secret)
2. Idempotency key check (`event_id`) - prevent replay attacks
3. IP whitelist (optional)
4. Rate limiting on webhook endpoint

**Status:** IMPLEMENTED

## Configuration Reference

### Environment Variables

#### Currently Configured ✅
```bash
# Auth Service (Current)
AUTH_SERVICE_URL=http://localhost:8081
AUTH_TIMEOUT=2s
AUTH_INTERNAL_SECRET=

# RevenueCat Webhooks (Implemented)
WEBHOOK_SECRET=whsec_xxx
WEBHOOK_IP_WHITELIST=1.2.3.4,5.6.7.8

# Postgres (Implemented)
DATABASE_URL=postgres://user:pass@host/db
TEST_DATABASE_URL=postgres://user:pass@host/test_db
```

#### Will Be Added (Future)
```bash
# Zitadel (Wave 2)
ZITADEL_ISSUER_URL=https://zitadel.example.com
ZITADEL_AUDIENCE=gateway.example.com
JWT_CACHE_TTL=5m

# Redis (Wave 3)
REDIS_ADDR=redis:6379
REDIS_PASSWORD=

# Feature Flags (Wave 5)
USE_ZITADEL_AUTH=false
USE_REVENUECAT_BILLING=false
```

#### Will Be Removed (After Migration)
```bash
AUTH_SERVICE_URL=        # Remove after Wave 2
AUTH_INTERNAL_SECRET=    # Remove after Wave 2
AUTH_TIMEOUT=           # Remove after Wave 2
```

## Database Schema (Already Implemented)

See [`docs/billing.md`](../../docs/billing.md) for complete schema documentation.

| Table | Status |
|-------|--------|
| `users` | ✅ Created |
| `usage_logs` | ✅ Created |
| `processed_webhooks` | ✅ Created |

## Open Questions

1. [ ] Exact credit-to-dollar ratios for each tier
2. [ ] TimescaleDB retention policy for usage_logs (if used)
3. [ ] Credit pack pricing and denominations
4. [ ] Rollback procedure if issues arise
5. [ ] Whether to use TimescaleDB for usage_logs

## References

- Implementation Plan: [`.claude/plans/auth-billing-migration.md`](./plans/auth-billing-migration.md)
- Auth Documentation: [`docs/auth.md`](../../docs/auth.md)
- Billing Documentation: [`docs/billing.md`](../../docs/billing.md)
- Configuration: [`docs/config.md`](../../docs/config.md)
- Zitadel Documentation: https://zitadel.com/docs
- RevenueCat Webhooks: https://www.revenuecat.com/reference/webhooks

# Gateway Authentication & Billing Migration Design

**Date:** 2025-02-23
**Status:** Partially Complete - Waves 1-3 Done, Wave 4-5 Remaining

## Implementation Status

| Wave | Status | Notes |
|------|--------|-------|
| 1: Foundation & Database | ✅ Complete | Migrations working, tests passing |
| 2: Zitadel Authentication | ✅ Complete | JWKS validation, middleware working |
| 3: RevenueCat Webhooks | ✅ Complete | HMAC validation, event handling done |
| 4: Rate Limiting & Quotas | ⏳ TODO | Only stub implementation exists |
| 5: Integration & Rollout | ⏳ TODO | Feature flags, monitoring pending |

## Remaining Work

The following sections describe what still needs to be implemented. See [`.claude/plans/auth-billing-migration.md`](./plans/auth-billing-migration.md) for detailed tasks.

### Wave 4: Rate Limiting & Quotas

**Current State:** `internal/billing/quota.go` exists but is a stub with TODO comments.

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

### Wave 5: Integration & Rollout

**Current State:** Not started.

**What Needs Implementation:**

1. **Feature Flags** - `USE_ZITADEL_AUTH`, `USE_REVENUECAT_BILLING`
2. **Monitoring & Metrics** - auth duration, webhook duration, rate limit denials, credit balance
3. **Migration Guide** - step-by-step rollout process
4. **Cleanup** - remove old auth service code after cutover

## Historical Design Context

The following sections contain the original design decisions. These are already implemented and documented in `docs/`.

### Authentication: Zitadel Direct Integration ✅

**Decision:** Users authenticate directly with Zitadel, gateway validates JWTs locally using JWKS.

```
Client ──OIDC+PKCE──▶ Zitadel ──JWT──▶ Gateway (JWKS validation)
```

**Implemented in:** `internal/auth/` - See [`docs/auth.md`](../../docs/auth.md)

### Token Format: Bearer JWT ✅

**Decision:** Clients send `Authorization: Bearer <jwt>` header.

**Implemented in:** `internal/auth/middleware.go` - See [`docs/auth.md`](../../docs/auth.md)

### Billing: RevenueCat Direct Integration ✅

**Decision:** Gateway receives RevenueCat webhooks, validates them, and manages credits/tiers.

**Implemented in:** `internal/billing/webhook_impl.go` - See [`docs/billing.md`](../../docs/billing.md)

### Data Storage: Postgres ✅

**Decision:** Postgres with TimescaleDB extension for time-series usage analytics.

**Implemented in:** `internal/billing/migrations.go` - See [`docs/billing.md`](../../docs/billing.md)

### Webhook Validation ✅

**Multi-layer security implemented:**
1. HMAC signature validation (RevenueCat shared secret)
2. Idempotency key check (`event_id`) - prevent replay attacks
3. IP whitelist (optional)
4. Rate limiting on webhook endpoint

**Implemented in:** `internal/billing/webhook_impl.go`

## Configuration Reference

### Environment Variables (Already Configured)

```bash
# Zitadel ✅ IMPLEMENTED
ZITADEL_ISSUER_URL=https://zitadel.example.com
ZITADEL_AUDIENCE=gateway.example.com
JWT_CACHE_TTL=5m

# RevenueCat ✅ IMPLEMENTED
WEBHOOK_SECRET=whsec_xxx
WEBHOOK_IP_WHITELIST=1.2.3.4,5.6.7.8

# Postgres ✅ IMPLEMENTED
DATABASE_URL=postgres://user:pass@host/db
TEST_DATABASE_URL=postgres://user:pass@host/test_db

# Redis ⏳ NEEDED FOR WAVE 4
REDIS_ADDR=redis:6379
REDIS_PASSWORD=

# Feature Flags ⏳ NEEDED FOR WAVE 5
USE_ZITADEL_AUTH=false
USE_REVENUECAT_BILLING=false
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
2. [ ] TimescaleDB retention policy for usage_logs
3. [ ] Credit pack pricing and denominations
4. [ ] Rollback procedure if issues arise

## References

- Implementation Plan: [`.claude/plans/auth-billing-migration.md`](./plans/auth-billing-migration.md)
- Auth Documentation: [`docs/auth.md`](../../docs/auth.md)
- Billing Documentation: [`docs/billing.md`](../../docs/billing.md)
- Configuration: [`docs/config.md`](../../docs/config.md)
- Zitadel Documentation: https://zitadel.com/docs
- RevenueCat Webhooks: https://www.revenuecat.com/reference/webhooks

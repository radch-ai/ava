# Implementation Plan: Gateway Auth & Billing Migration

**Status:** Wave 1 Complete, Wave 2-5 Remaining
**Design:** [`.claude/brainstorming/auth-billing-migration.md`](../brainstorming/auth-billing-migration.md)
**Created:** 2025-02-23
**Updated:** 2025-02-24 (corrected implementation status)

## Implementation Status

| Wave | Focus | Status | Documentation |
|------|-------|--------|---------------|
| 1 | Foundation & Database | ✅ Complete | [`docs/billing.md`](../../docs/billing.md) |
| 2 | Zitadel Authentication | ❌ Not Started | [`docs/auth.md`](../../docs/auth.md) (planned) |
| 3 | RevenueCat Webhooks | ⚠️ Partial | [`docs/billing.md`](../../docs/billing.md) |
| 4 | Rate Limiting & Quotas | ⏳ TODO | See below |
| 5 | Integration & Rollout | ⏳ TODO | See below |

## Completed Work (Wave 1)

### Wave 1: Foundation & Database ✅

**Status:** Complete and tested

- ✅ Dependencies added (`pgx/v5`)
- ✅ Billing package structure created
- ✅ Migration functions (`RunMigrations`, `CheckMigrations`, `RunMigrationsWithPool`)
- ✅ Database schema (users, usage_logs, processed_webhooks)
- ✅ Idempotent migrations with transaction support
- ✅ Configuration updated (DATABASE_URL, TEST_DATABASE_URL)
- ✅ Tests: `internal/billing/migrations_test.go` (10/10 passing)

**Files:**
- `internal/billing/migrations.go` - Database migrations
- `internal/billing/migrations_test.go` - Migration tests

---

## Remaining Work

### Wave 2: Zitadel Authentication ❌

**Goal:** Replace auth service client with direct Zitadel JWKS validation.

**Current State:** Gateway uses HTTP-based auth service client (`internal/auth/client.go`).

#### Tasks

#### 2.1 JWKS Fetching & Caching
- [ ] Create `internal/auth/jwks.go`
- [ ] Implement JWKS fetching from Zitadel
- [ ] Implement in-memory caching with TTL
- [ ] Add HTTP middleware for JWKS refresh

#### 2.2 JWT Validation
- [ ] Create `internal/auth/validator.go`
- [ ] Implement signature verification using JWKS
- [ ] Implement claims validation (issuer, audience, expiration)
- [ ] Add unit tests for validation logic

#### 2.3 Middleware Integration
- [ ] Create `internal/auth/middleware.go`
- [ ] Extract Bearer token from Authorization header
- [ ] Call validator, set user context
- [ ] Handle errors (401, 403)

#### 2.4 Configuration
- [ ] Add Zitadel config to `config/`
- [ ] Add `ZITADEL_ISSUER_URL` env var
- [ ] Add `ZITADEL_AUDIENCE` env var
- [ ] Add `JWT_CACHE_TTL` env var

#### 2.5 Write Tests
- [ ] Unit tests for JWKS fetching
- [ ] Unit tests for JWT validation
- [ ] Unit tests for middleware
- [ ] Integration tests with mock Zitadel

#### 2.6 Remove Old Code
- [ ] Remove `internal/auth/client.go`
- [ ] Remove `AUTH_SERVICE_URL` from config
- [ ] Remove `AUTH_INTERNAL_SECRET` from config
- [ ] Remove `AUTH_TIMEOUT` from config

**Acceptance Criteria:**
- JWKS fetched and cached
- JWTs validated correctly
- Middleware sets user context
- Old auth service code removed
- All tests pass

---

### Wave 3: Complete RevenueCat Webhooks ⚠️

**Goal:** Implement quota manager with period-based rate limiting.

**Current State:** Webhook handler is complete (`internal/billing/webhook_impl.go`). Quota manager is a stub (`internal/billing/quota.go`).

#### Tasks

#### 3.1 Quota Management
- [ ] Implement period calculation logic (6-hour periods)
- [ ] Implement 10% buffer with cap at remaining credits
- [ ] Implement Redis caching for period limits
- [ ] Implement Redis caching for period usage
- [ ] Implement period rollover detection

#### 3.2 Pre-Request Check
- [ ] Check if user has sufficient credits
- [ ] Return 402 if insufficient
- [ ] Check if period limit available
- [ ] Return 429 if period exceeded
- [ ] Reserve credit before LLM call

#### 3.3 Post-Request Deduction
- [ ] Calculate actual credit cost
- [ ] Deduct from `users.credits_remaining`
- [ ] Insert into `usage_logs`
- [ ] Update Redis cache
- [ ] Handle refunds on error

#### 3.4 Tier Limits
- [ ] Store tier limits in config
- [ ] Check agent count before execution
- [ ] Check cron job count before scheduling
- [ ] Cache tier limits in Redis

**Tier Limits:**
| Tier | Agent Limit | Cron Limit |
|------|-------------|------------|
| free | 1 | 0 |
| pro | 5 | 10 |
| enterprise | unlimited | unlimited |

#### 3.5 Update LLM Proxy Handler
- [ ] Add pre-request quota check
- [ ] Add post-request credit deduction
- [ ] Add error handling for insufficient credits
- [ ] Add rate limit headers to responses

#### 3.6 Update Agent Handler
- [ ] Add agent count limit check
- [ ] Add cron job count limit check
- [ ] Return appropriate errors for tier limits

#### 3.7 Write Tests
- [ ] Unit tests for period calculation
- [ ] Unit tests for buffer calculation
- [ ] Unit tests for credit reservation
- [ ] Unit tests for credit deduction
- [ ] Integration tests for rate limiting flow
- [ ] Tests for tier limit enforcement

**Acceptance Criteria:**
- Period limits calculated correctly
- Rate limits enforced (429 responses)
- Credits deducted after LLM calls
- Agent limits enforced by tier
- All tests pass

---

### Wave 4: Integration ⏳

**Goal:** Wire up quota manager to existing handlers.

#### Tasks

- [ ] Integrate quota check into WebSocket handler
- [ ] Integrate quota check into LLM proxy
- [ ] Add metrics for quota operations
- [ ] Add circuit breaker for quota service
- [ ] Add graceful degradation on quota failures

---

### Wave 5: Integration & Rollout ⏳

**Goal:** Feature flags, monitoring, documentation, cleanup.

#### Tasks

#### 5.1 Feature Flag Implementation
- [ ] Implement `USE_ZITADEL_AUTH` flag logic
- [ ] Implement `USE_REVENUECAT_BILLING` flag logic
- [ ] Add graceful fallback to old system
- [ ] Add metrics for flag usage

#### 5.2 Monitoring & Observability
- [ ] Add `auth_validation_duration` metric
- [ ] Add `webhook_processing_duration` metric
- [ ] Add `rate_limit_denials` metric
- [ ] Add `credit_balance` gauge
- [ ] Set up alerts for signature failures
- [ ] Set up alerts for high denials

#### 5.3 Documentation Updates
- [x] ~~Update `docs/config.md` with new env vars~~ ✅ Done
- [x] Update `docs/auth.md` ✅ Done
- [x] Update `docs/billing.md` ✅ Done
- [ ] Create `docs/migration.md` for rollout guide
- [ ] Update README with new architecture
- [ ] Create API documentation for webhooks

#### 5.4 Migration Guide
- [ ] Create step-by-step migration guide
- [ ] Document feature flag rollout process
- [ ] Document rollback procedure
- [ ] Create monitoring dashboard guide

#### 5.5 Testing & Validation
- [ ] Run full test suite
- [ ] Run integration tests
- [ ] Manual testing with feature flags
- [ ] Load testing for rate limiting
- [ ] Security review of webhook handler
- [ ] Security review of JWT validation

#### 5.6 Gradual Rollout
- [ ] Deploy to staging
- [ ] Enable for 10% of traffic
- [ ] Monitor metrics and errors
- [ ] Enable for 50% of traffic
- [ ] Monitor metrics and errors
- [ ] Enable for 100% of traffic

#### 5.7 Cleanup
- [ ] Remove old auth client code (`internal/auth/client.go`)
- [ ] Remove old auth middleware (if parallel version exists)
- [ ] Remove feature flags
- [ ] Update tests
- [ ] Remove old environment variables

#### 5.8 Verification
- [ ] All tests passing
- [ ] No increase in error rates
- [ ] Latency within SLA
- [ ] Security review complete
- [ ] Documentation complete

**Acceptance Criteria:**
- All waves complete
- All tests passing
- Documentation updated
- Old code removed
- Monitoring in place

---

## Configuration Reference

### Environment Variables

#### Currently Configured ✅
```bash
# Auth Service (Current - will be removed)
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

#### To Be Removed After Full Migration
```bash
AUTH_SERVICE_URL=        # Remove after Wave 5 cleanup
AUTH_INTERNAL_SECRET=    # Remove after Wave 5 cleanup
AUTH_TIMEOUT=           # Remove after Wave 5 cleanup
```

---

## Dependencies

### Already Added ✅
```go
require (
    github.com/jackc/pgx/v5 v5.x.x
    github.com/rs/zerolog v1.x.x
    golang.org/x/time/rate v0.x.x
)
```

### Will Be Added (Future)
```go
require (
    github.com/zitadel/oidc/v3 v3.x.x  // Wave 2
    github.com/golang-jwt/jwt/v5 v5.x.x  // Wave 2
    github.com/redis/go-redis/v9 v9.x.x  // Wave 3
)
```

### Infrastructure Requirements
- ✅ Postgres
- ⏳ Redis instance (needed for Wave 3)
- ❌ Zitadel instance (needed for Wave 2)
- ✅ RevenueCat account (external)

---

## Rollout Plan

### Week 1: ✅ Complete
- ✅ Wave 1 (database, migrations)

### Week 2: Zitadel Authentication (Pending)
- [ ] Implement Wave 2 (JWKS validation)
- [ ] Deploy to staging
- [ ] Test with real Zitadel instance
- [ ] Enable for internal users first

### Week 3: Quota Manager (Pending)
- [ ] Implement Wave 3 (quota manager)
- [ ] Deploy to staging
- [ ] Test rate limiting
- [ ] Test credit deductions

### Week 4: Integration (Pending)
- [ ] Deploy Wave 4 (integration)
- [ ] Test end-to-end flow

### Week 5: Full Cutover (Pending)
- [ ] Deploy Wave 5 (integration)
- [ ] Enable feature flags gradually
- [ ] Monitor for issues

### Week 6: Cleanup (Pending)
- [ ] Remove old auth service code
- [ ] Remove feature flags
- [ ] Final documentation update

---

## Success Criteria

- [x] All unit tests passing (Wave 1)
- [x] All integration tests passing (Wave 1)
- [ ] Auth validation p99 < 50ms (Wave 2)
- [ ] Webhook processing p99 < 500ms (Wave 3)
- [ ] Rate limiting functional (Wave 3)
- [ ] No increase in error rate
- [x] Documentation complete (Waves 1-3)
- [ ] Security review passed
- [ ] Old code removed

## Open Questions

1. [ ] Exact credit-to-dollar ratios for each tier
2. [ ] Whether to use TimescaleDB for usage_logs
3. [ ] Credit pack pricing and denominations
4. [ ] Rollback procedure if issues arise

## References

- Design Document: [`.claude/brainstorming/auth-billing-migration.md`](../brainstorming/auth-billing-migration.md)
- Auth Documentation: [`docs/auth.md`](../../docs/auth.md)
- Billing Documentation: [`docs/billing.md`](../../docs/billing.md)
- Configuration: [`docs/config.md`](../../docs/config.md)
- Zitadel Docs: https://zitadel.com/docs
- RevenueCat Docs: https://www.revenuecat.com/docs

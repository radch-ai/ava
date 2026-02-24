# Implementation Plan: Gateway Auth & Billing Migration

**Status:** Waves 1-3 Complete, Wave 4-5 Remaining
**Design:** [`.claude/brainstorming/auth-billing-migration.md`](../brainstorming/auth-billing-migration.md)
**Created:** 2025-02-23

## Implementation Status

| Wave | Focus | Status | Documentation |
|------|-------|--------|---------------|
| 1 | Foundation & Database | ✅ Complete | [`docs/billing.md`](../../docs/billing.md) |
| 2 | Zitadel Authentication | ✅ Complete | [`docs/auth.md`](../../docs/auth.md) |
| 3 | RevenueCat Webhooks | ✅ Complete | [`docs/billing.md`](../../docs/billing.md) |
| 4 | Rate Limiting & Quotas | ⏳ TODO | See below |
| 5 | Integration & Rollout | ⏳ TODO | See below |

## Completed Work (Waves 1-3)

The following components are fully implemented and tested:

### Wave 1: Foundation & Database ✅
- ✅ Dependencies added (`zitadel/oidc`, `golang-jwt/jwt`, `pgx/v5`)
- ✅ Billing package structure created
- ✅ Migration command (`cmd/migrate/main.go`)
- ✅ Database schema (users, usage_logs, processed_webhooks)
- ✅ TimescaleDB hypertable support
- ✅ Configuration updated (DATABASE_URL, TEST_DATABASE_URL)
- ✅ Tests passing

### Wave 2: Zitadel Authentication ✅
- ✅ JWKS fetching and caching
- ✅ JWT validation (signature, claims, expiration)
- ✅ Bearer token extraction middleware
- ✅ Session caching in Redis
- ✅ Configuration (ZITADEL_ISSUER_URL, ZITADEL_AUDIENCE)
- ✅ Tests passing

### Wave 3: RevenueCat Webhooks ✅
- ✅ HMAC signature validation
- ✅ JSON parsing and event routing
- ✅ Idempotency check (event_id deduplication)
- ✅ IP whitelist support
- ✅ Rate limiting (60 req/min)
- ✅ Event handlers (INITIAL_PURCHASE, RENEWAL, CANCELLATION, REFUND, PRODUCT_CHANGE)
- ✅ Configuration (WEBHOOK_SECRET, WEBHOOK_IP_WHITELIST)
- ✅ Tests passing

---

## Remaining Work

### Wave 4: Rate Limiting & Quotas ⏳

**Goal:** Implement credit-based rate limiting with 6-hourly periods.

**Current State:** `internal/billing/quota.go` exists but is a stub.

#### Tasks

#### 4.1 Implement Quota Management
- [ ] Implement period calculation logic (6-hour periods)
- [ ] Implement 10% buffer with cap at remaining credits
- [ ] Implement Redis caching for period limits
- [ ] Implement Redis caching for period usage
- [ ] Implement period rollover detection

#### 4.2 Implement Pre-Request Check
- [ ] Check if user has sufficient credits
- [ ] Return 402 if insufficient
- [ ] Check if period limit available
- [ ] Return 429 if period exceeded
- [ ] Reserve credit before LLM call

#### 4.3 Implement Post-Request Deduction
- [ ] Calculate actual credit cost
- [ ] Deduct from `users.credits_remaining`
- [ ] Insert into `usage_logs`
- [ ] Update Redis cache
- [ ] Handle refunds on error

#### 4.4 Implement Tier Limits
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

#### 4.5 Update LLM Proxy Handler
- [ ] Add pre-request quota check
- [ ] Add post-request credit deduction
- [ ] Add error handling for insufficient credits
- [ ] Add rate limit headers to responses

#### 4.6 Update Agent Handler
- [ ] Add agent count limit check
- [ ] Add cron job count limit check
- [ ] Return appropriate errors for tier limits

#### 4.7 Write Tests
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
- [x] Create `docs/auth.md` ✅ Done
- [x] Create `docs/billing.md` ✅ Done
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

#### 5.6 Gradual Rollout
- [ ] Deploy to staging
- [ ] Enable for 10% of traffic
- [ ] Monitor metrics and errors
- [ ] Enable for 50% of traffic
- [ ] Monitor metrics and errors
- [ ] Enable for 100% of traffic

#### 5.7 Cleanup
- [ ] Remove old auth client code (`internal/auth/client.go` if exists)
- [ ] Remove old auth middleware (if parallel version exists)
- [ ] Remove feature flags
- [ ] Update tests
- [ ] Remove old environment variables (AUTH_SERVICE_URL, AUTH_INTERNAL_SECRET)

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

#### Already Configured ✅
```bash
# Zitadel Authentication
ZITADEL_ISSUER_URL=https://zitadel.example.com
ZITADEL_AUDIENCE=gateway.example.com
JWT_CACHE_TTL=5m

# RevenueCat Billing
WEBHOOK_SECRET=whsec_xxx
WEBHOOK_IP_WHITELIST=1.2.3.4,5.6.7.8

# Postgres
DATABASE_URL=postgres://user:pass@host/db
TEST_DATABASE_URL=postgres://user:pass@host/test_db
```

#### Still Needed ⏳
```bash
# Redis (for Wave 4 rate limiting)
REDIS_ADDR=redis:6379
REDIS_PASSWORD=

# Feature Flags (for Wave 5 rollout)
USE_ZITADEL_AUTH=false
USE_REVENUECAT_BILLING=false
```

### To Be Removed After Full Migration
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
    github.com/zitadel/oidc/v3 v3.x.x
    github.com/golang-jwt/jwt/v5 v5.x.x
    github.com/redis/go-redis/v9 v9.x.x
    github.com/jackc/pgx/v5 v5.x.x
)
```

### Infrastructure Requirements
- ✅ Postgres with TimescaleDB extension
- ⏳ Redis instance (needed for Wave 4)
- ✅ Zitadel instance (external)
- ✅ RevenueCat account (external)

---

## Rollout Plan (Updated)

### Week 1-3: ✅ Complete
- ✅ Wave 1 (database, migrations)
- ✅ Wave 2 (Zitadel auth)
- ✅ Wave 3 (RevenueCat webhooks)

### Week 4: Rate Limiting (Pending)
- [ ] Deploy Wave 4 (quotas and rate limiting)
- [ ] Enable for new users only
- [ ] Monitor rate limit denials

### Week 5: Full Cutover (Pending)
- [ ] Deploy Wave 5 (integration)
- [ ] Enable feature flags for all users
- [ ] Monitor for 1 week

### Week 6: Cleanup (Pending)
- [ ] Remove old auth service code
- [ ] Remove feature flags
- [ ] Final documentation update

---

## Success Criteria

- [x] All unit tests passing (Waves 1-3)
- [x] All integration tests passing (Waves 1-3)
- [ ] Auth validation p99 < 50ms
- [ ] Webhook processing p99 < 500ms
- [ ] Rate limiting functional
- [ ] No increase in error rate
- [x] Documentation complete (Waves 1-3)
- [ ] Security review passed
- [ ] Old code removed

## Open Questions

1. [ ] Exact credit-to-dollar ratios for each tier
2. [ ] TimescaleDB retention policy for usage_logs
3. [ ] Credit pack pricing and denominations
4. [ ] Rollback procedure if issues arise

## References

- Design Document: [`.claude/brainstorming/auth-billing-migration.md`](../brainstorming/auth-billing-migration.md)
- Auth Documentation: [`docs/auth.md`](../../docs/auth.md)
- Billing Documentation: [`docs/billing.md`](../../docs/billing.md)
- Configuration: [`docs/config.md`](../../docs/config.md)
- Zitadel Docs: https://zitadel.com/docs
- RevenueCat Docs: https://www.revenuecat.com/docs

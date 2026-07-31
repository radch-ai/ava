# LLM Proxy Design Decisions

**Date:** 2025-02-12

## Key Architectural Decisions

### 1. Execution Order

Quota check before routing is critical to:
- Avoid wasting router calls when quota exhausted
- Get user context (tier, remaining, addon status) needed for routing
- Block early with upgrade prompt instead of generic 503

### 2. Router Model Selection

Uses a cheap model (gpt-4o-mini by default) to decide which LLM model to use based on:
- Query complexity (simple vs. complex reasoning needed)
- User's plan tier (basic vs premium)
- Quota remaining percentage
- Days until monthly reset
- Add-on credit status

### 3. Multi-Layer Fallback

Usage tracking must never lose data:
1. **Primary** - PostgreSQL for persistent storage
2. **Secondary** - Redis for high-speed buffering
3. **Tertiary** - Disk queue as last resort

When primary fails, gateway returns 503 but continues queueing to prevent data loss.

### 4. Circuit Breaker Pattern

Provider health tracked per `provider:model` combination:
- **Healthy** - Route requests normally
- **Degraded** - Track closely, recent failures
- **Unhealthy** - Circuit open, skip provider

Auto-recovery via probe requests after cooldown period.

### 5. Service Pause Mechanism

Gateway can be paused via internal state:
- Returns 503 with `Retry-After` header
- Usage events queue to disk instead of being lost
- Auto-resumes when storage recovers

### 6. Model Tier System

| Tier | Access | Cost |
|-------|----------|-------|
| Economy | Basic + Premium | Lowest |
| Premium | Basic + Premium | Medium |
| Ultra | Premium only | Highest |

This allows upselling premium tier for complex tasks while keeping basic plan affordable.

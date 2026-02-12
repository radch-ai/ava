# Model Router

The router intelligently selects which LLM model to use for each request.

## Decision Flow

```
Request ──▶ User Context ──▶ Quota Check ──▶ Router LLM ──▶ Model Selection
                                                           │
                                                           ▼
                                                   Selected Model
```

## Router LLM

A cheap, fast model (e.g., gpt-4o-mini) makes routing decisions:

**Input:**
```json
{
  "user_plan": "premium",
  "quota_remaining_monthly": 45.00,
  "quota_remaining_6h": 5.00,
  "query_complexity": "high",
  "user_preference": "quality"
}
```

**Output:**
```json
{
  "model": "claude-3-5-sonnet",
  "reason": "high_complexity_query_with_quality_preference"
}
```

## Selection Logic

| Factor | Economy | Premium | Ultra |
|--------|----------|----------|--------|
| **Base plan** | ✓ | ✓ | ✓ |
| **Premium plan** | ✓ | ✓ | ✓ |
| **Low quota (<10%)** | Economy only | Economy only | Premium |
| **High complexity** | Premium | Premium | Ultra |
| **User preference** | Respects | Respects | Respects |

## Configuration

```yaml
router:
  model: gpt-4o-mini     # Which model makes routing decisions
  timeout: 5s              # Max time for routing decision
  max_retries: 2            # Retry on routing failures
```

## Fallback

If router fails:
1. Return configured default for user's plan
2. Log error for monitoring
3. Continue request (don't block)

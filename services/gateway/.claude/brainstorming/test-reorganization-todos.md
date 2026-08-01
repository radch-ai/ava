# Test Reorganization - Remaining TODOs

Date: 2026-02-15

## Completed
- ✅ Reorganized all tests from `internal/` to `tests/unit/` structure
- ✅ Created `tests/integration/` and `tests/e2e/` directories
- ✅ Created `tests/testutil/` with common test helpers
- ✅ Created `tests/docker-compose.test.yml` with Redis and mock auth service
- ✅ Updated Makefile with test targets matching auth service
- ✅ All unit tests passing (26 packages)
- ✅ All E2E tests passing (6/6)
- ✅ 11/13 integration tests passing

## Remaining TODOs

### Integration Test Fixes

**File**: `tests/integration/websocket_test.go`

Two tests fail because agents don't have LLM clients configured:

1. **`TestAgentEventForwarding`** (line ~221)
   - Error: `expected at least 2 messages, got 1`
   - Cause: Agent has no LLM client to generate responses
   - Fix: Add mock LLM client or adjust test expectations

2. **`TestPromptHandling`** (line ~260)
   - Error: `expected at least 2 messages, got 1`
   - Cause: Same as above
   - Fix: Add mock LLM client or adjust test expectations

### Suggested Fix Approach

Option A: Add Mock LLM Client
```go
mockLLM := &MockLLMClient{
    Responses: []MockResponse{
        {
            Message: agent.NewTextMessage(agent.RoleAssistant, "Test response"),
        },
    },
}
// Configure agent manager with mock LLM
```

Option B: Adjust Test Expectations
- Accept that without LLM, only `connected` event is sent
- Remove assertion for agent response messages

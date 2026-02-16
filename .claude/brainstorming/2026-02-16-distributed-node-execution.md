# Distributed Node Execution System

**Status:** Design Approved
**Date:** 2026-02-16
**Type:** Architecture Feature

## Overview

Enable users to run agent tools on their own devices (nodes) instead of gateway infrastructure. Users install a node app (Windows/Mac/Linux/iOS), which maintains a WebSocket connection to the gateway. Tools execute in Docker containers on the node for isolation.

## Problem Being Solved

**Original Plan:** Run tools in warm containers on gateway infrastructure, spinning up/down based on usage (5-10min inactivity timeout).

**Issues with Original Plan:**
- High infrastructure cost at early stage
- Complex container lifecycle management
- Micro-calls cause frequent spin-up/spin-down

**New Approach:** Users run tools on their own devices. Gateway orchestrates, nodes execute.

## Key Principles

1. **Agent on Gateway** - Agent runtime stays centralized (~$0.06/month per agent in infra costs)
2. **Tools on Nodes** - Heavy execution happens on user devices (their CPU/RAM/Docker)
3. **Private Nodes** - Each user's agent only accesses their own nodes (no shared execution)
4. **Node Agnostic** - Gateway treats user nodes and managed nodes identically
5. **Lazy Discovery** - Tool health discovered on-demand (no push/heartbeat complexity)

## Architecture

```
┌─────────────┐     WebSocket      ┌─────────────────────────────┐
│  Web/Mobile │ ───────────────────>│  Nevaria Gateway           │
│     App     │                     │  ┌─────────────────────────┐ │
└─────────────┘                     │  │ Agent Runtime (your    │ │
                                     │  │ infra ~$0.06/agent)    │ │
                                     │  └─────────────────────────┘ │
                                     │  ┌─────────────────────────┐ │
                                     │  │ Node Connection Manager  │ │
                                     │  └─────────────────────────┘ │
                                     │  ┌─────────────────────────┐ │
                                     │  │ Tool Cache (Redis 2-5m) │ │
                                     │  └─────────────────────────┘ │
                                     └─────────────────────────────┘
                                                   │
                                                   │ WebSocket (node → gateway)
                                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                              Nodes                                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ User's PC    │  │ Managed Node │  │ iOS/Android │              │
│  │ (Mac/Win/Linux) │ │ (VPS - you) │  │ (Mobile App) │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
│                                                                     │
│  Tools run in Docker containers for isolation                      │
└─────────────────────────────────────────────────────────────────────┘
```

## Tool Naming Convention

Tools exposed to LLM have node-specific prefixes:

```
Format: {platform}_{node_uuid}_{tool_name}[_variant]
```

**Examples:**
```
mac_a1b2c3_bash           # Docker isolated (default for user nodes)
mac_a1b2c3_bash_onhost    # Direct on host (opt-in, advanced)
mac_a1b2c3_read_file      # Docker with volume mounts
ios_d4e5f6_camera         # iOS native tool
managed_x1y2z3_bash       # Managed node, direct host (default)
managed_x1y2z3_bash_isolated  # Managed node, Docker (opt-in)
weather_search            # Cloud tool (no prefix)
web_search                # Cloud tool (no prefix)
```

**Routing:**
1. Agent fetches available tools from cache
2. LLM selects tool
3. Gateway parses tool name → extracts node_uuid
4. Gateway sends execution request to that node
5. If node offline, tools don't appear in list

## Node Discovery & Health

**Lazy Discovery - piggyback on existing triggers:**

| Trigger | Action | Cache Update |
|---------|--------|--------------|
| User WebSocket connect | Query all user's nodes | ✅ Set cache |
| Heartbeat fires | Query all user's nodes | ✅ Set cache |
| Cron job fires | Query all user's nodes | ✅ Set cache |
| Tool execution fails | Query that specific node | ✅ Set cache |
| Node reconnects | Query that node | ✅ Set cache |

**Redis cache:**
```
Key: user_tools:{user_id}
Value: ["mac_a1b2c3_bash", "mac_a1b2c3_read_file", "ios_d4e5f6_camera", "weather_search"]
TTL: 2-5 minutes
```

**Cache reset behavior:**
- Always reset: Explicit health check, tool failure, node reconnect
- Extend TTL only: Successful execution, heartbeat

## Protocol

### Node → Gateway Connection

```
ws://gateway.com/node/connect?token={session_token}
```

### Messages (Node → Gateway)

**Register tools on connect:**
```json
{
  "type": "register",
  "node_id": "mac_a1b2c3",
  "platform": "mac",
  "tools": ["bash", "read_file", "write_file", "bash_onhost"]
}
```

**Health check response:**
```json
{
  "type": "health_response",
  "node_id": "mac_a1b2c3",
  "tools": ["bash", "read_file", "bash_onhost"],
  "status": "healthy"
}
```

### Messages (Gateway → Node)

**Execute tool:**
```json
{
  "type": "execute",
  "tool": "bash",
  "args": {"command": "ls -la"},
  "call_id": "tool_123"
}
```

## Security & Isolation

**User nodes:**
- Docker containers for isolation (default)
- User configures volume mounts (whitelisted directories only)
- `_onhost` tools disabled by default (opt-in)

**Managed nodes:**
- Pre-configured VPS running same node app
- All tools enabled by default (user can reset/rebuild)
- You manage infrastructure, user pays add-on fee

**Mobile nodes:**
- No Docker (OS limitation)
- Native tools only (camera, gallery, etc.)
- Limited capabilities

## Plan Tiers

| Resource | Lite ($20/mo) | Standard ($50/mo) | Pro ($100/mo) |
|----------|---------------|-------------------|---------------|
| Agents | 2 | 7 | 20 |
| Heartbeat interval | 2 hours | 30 minutes | 15 minutes |
| Cron jobs | 7 | 20 | 50 |
| Nodes per user | 3 | 5 | 10 |
| Managed nodes | Not available | Available add-on | Included (1 node) |
| LLM credits | $10 | $25 | $50 |

**Notes:**
- All limits can be increased as paid add-ons
- No "unlimited" tiers - business/enterprise will have large defined limits
- Heartbeat vs Cron: Heartbeat = recurring check-in, Cron = scheduled tasks
- Managed node specs for Pro: ~1 vCPU, 2GB RAM (cost to you <$5)

## Node App Structure

**Separate repository** from gateway.

**Components:**
- WebSocket Client - Connects to gateway, handles protocol
- Tool Registry - Lists available tools
- Docker Manager - Spawns containers for isolated tools
- File Mount Manager - Manages user-configured volume mounts
- Auth Module - Session token from login
- Health Handler - Responds to health_check queries

**Platform support:**
- Desktop (Mac/Windows/Linux): Full app, Docker required
- Mobile (iOS/Android): Limited tools, no Docker
- Managed Nodes: Same app, pre-configured

**User setup flow:**
1. Download app → Sign in → Auto-connect to gateway
2. Configure allowed directories (for volume mounts)
3. Enable/disable tools (default: Docker tools enabled, `_onhost` disabled)
4. App stays running in background, maintains persistent WebSocket

## Implementation Phases

### Phase 1: Gateway Node Protocol (Core)
- WebSocket connection handler for nodes (`/node/connect`)
- Node registry (track connected nodes by user_id)
- Tool discovery endpoint (query node for available tools)
- Tool execution routing (gateway → node → response)
- Basic Redis caching for tool lists

### Phase 2: Agent Integration
- Update Agent to include node tools in available tools list
- Route tool calls to appropriate node
- Handle node offline scenarios (tools disappear from list)
- Lazy health discovery on user/heartbeat/cron triggers

### Phase 3: Tool Caching & Health
- Redis tool cache by user_id (2-5 min TTL)
- Cache refresh logic (connect, heartbeat, cron, failure)
- Tool name parsing (extract node_uuid from tool name)
- Fallback to cloud-only tools when no nodes available

### Phase 4: Plan Limits Enforcement
- Agent limits per plan
- Node limits per plan
- Heartbeat intervals per plan
- Cron job limits per plan
- Managed node allocation

## Alternatives Considered

| Option | Description | Why Rejected |
|--------|-------------|--------------|
| Agent on Node | Agent runs on user's device | Would need complex state sync, harder to manage, less control |
| Shared Node Pool | Any node can execute anyone's tools | Complex security/isolation, not our model |
| Push Health Updates | Node proactively sends tool changes | Missed checkin handling too complex |
| Managed Fallback | Paid nodes when user's offline | You said "no fallback" |

## Open Questions (Parked)

- Streaming vs fire-and-forget for tool results
- Long-running tool handling (timeouts, async)
- Exact TTL value (2-5 min range decided, specific value TBD)

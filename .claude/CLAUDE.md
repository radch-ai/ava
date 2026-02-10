# Claude Code Context - gateway

## Project Context Files

**IMPORTANT:**
- Always read `README.md` first to understand project purpose
- `.claude/brainstorming/` - Active planning, decisions in progress
- `.claude/plans/` - Executable implementation plans
- `docs/` - Completed work, proper project documentation

## Documentation Workflow

**When work is complete:**
1. Move details from `.claude/brainstorming/` → `docs/`
2. Remove from brainstorming files
3. Keep only pending/in-progress work in `.claude/`

**docs/ content guidelines:**
- Clean reference documentation - explanations, architecture, API references, configuration guides
- Remove planning artifacts: todo lists, status tracking, "next steps", discussion notes
- No process traces: no "we considered X but chose Y", decision-making context
- No references to .claude/ files or internal planning
- Stand-alone content that any developer can read without AI context

**Directory structure:**
```
.claude/
├── CLAUDE.md              # This file
├── brainstorming/         # Active planning, decisions in progress
└── plans/                 # Executable implementation plans

docs/                      # Completed work (proper documentation)
├── foundation.md          # Phase 0 documentation
└── ... (more as completed)
```

## Tech Stack

- **Language**: Go

## Development Workflow

**CRITICAL: Follow parallel subagent development for faster iteration**

### For Multi-File Tasks (Recommended)

When working on multiple independent code areas, launch parallel `golang-expert` subagents:

```bash
# Example: 3 parallel subagents for different files
Agent 1: internal/websocket/handler.go   + TDD
Agent 2: internal/server/server.go       + TDD
Agent 3: Final verification agent
```

**Parallel subagent template:**
1. **Identify independent tasks** - Files/packages that can be worked on separately
2. **Launch subagents in parallel** - Use single message with multiple Task tool calls
3. **Each subagent follows TDD** - RED-GREEN-REFACTOR cycle
4. **Final verification** - One agent checks all work together

### Required Skills (Invoke Inside Subagents)

Every subagent MUST use these skills:

| Skill | When to Use |
|-------|-------------|
| `superpowers:test-driven-development` | ALL code changes - RED-GREEN-REFACTOR |
| `universal-dev-standards:code-architect` | Design decisions before implementation |
| `universal-dev-standards:code-reviewer` | Review code after completion |

### TDD Cycle (Non-Negotiable)

```
RED   → Write failing test, verify it fails correctly
GREEN → Minimal code to pass
REFACTOR → MUST check for cleanup opportunities
```

**REFACTOR is MANDATORY** - Even if code looks clean, you MUST:
- Check for duplication
- Look for extraction opportunities
- Review naming
- Consider simpler alternatives

**Red flags that mean restart:**
- Test passes immediately (wrong test)
- Code written before test
- Skipped verification phases
- Skipped refactor check ("code looked fine so I skipped")

## Quick Commands

```bash
# Build
make build

# Run
make run

# Test
make test

# Regenerate project details from parent
make generate
```

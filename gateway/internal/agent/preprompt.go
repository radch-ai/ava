package agent

// The lane-level pre-prompt automation: before a prompt-kind item is
// claimed, one checker measures the projected complete request against
// the resolved model's window and — compact threshold first, then the
// tool-mass shake threshold below it — runs the existing engines in
// their itemless forms. The prompt stays queued while any action runs;
// it claims only after the checker settles, so its user row commits
// once, directly into the resulting thread. Every seam failure of the
// checker's own degrades to a nil handoff: the ordinary turn path then
// behaves exactly as before.

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/schema"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

const (
	// prePromptCompactThreshold is the whole-context share of the
	// resolved window at which the compaction core runs
	// unconditionally, with no preceding shake.
	prePromptCompactThreshold = 70 // percent of the resolved window
	// prePromptShakeToolThreshold is the tool-mass share of the
	// resolved window above which the durable shake runs — but only
	// below the compact threshold.
	prePromptShakeToolThreshold = 25 // percent of the resolved window
	// perMessageOverheadTokens is the fixed framing allowance each
	// projected message adds to the whole estimate, absorbing
	// role/structure overhead across wire shapes.
	perMessageOverheadTokens = 8
)

// prePromptHandoff carries the checker's single read and resolutions
// to the turn: the projected records (post-rotation or post-shake when
// either ran, plain otherwise — the incoming prompt is NOT among
// them), the resolved model, persona instruction, and tool set. The
// no-action path performs no second thread read and no second
// resolution; the turn appends the incoming prompt message and runs.
type prePromptHandoff struct {
	item        *queuedItem
	records     []backend.AgentThreadMessage
	cfg         backend.ResolvedModel
	instruction string
	tools       backend.TurnTools
}

// prePromptTokens measures the projected complete request. Per record,
// mirror mapAgentThread's text forms exactly (user: content;
// assistant: content + each call's name+arguments; tool: tool name +
// content); the tool-role sum doubles as toolMass — already-elided
// rows project as ~50-byte placeholders and weigh accordingly.
func prePromptTokens(instruction string, infos []*schema.ToolInfo,
	records []backend.AgentThreadMessage, prompt string) (whole, toolMass int) {
	whole = estimateTokens(instruction)
	for _, info := range infos {
		raw, _ := json.Marshal(info)
		whole += estimateTokens(string(raw))
	}
	for _, m := range records {
		text := m.Content
		switch m.Role {
		case apitypes.ThreadRoleUser:
			// content alone
		case apitypes.ThreadRoleAssistant:
			for _, tc := range m.ToolCalls {
				text += tc.Name + tc.Arguments
			}
		default: // tool: the envelope's model-facing text
			text = m.ToolName + m.Content
			toolMass += estimateTokens(text)
		}
		whole += estimateTokens(text) + perMessageOverheadTokens
	}
	whole += estimateTokens(prompt)
	return whole, toolMass
}

// prePromptCheck runs the lane-level automation for one queued prompt
// item before it is claimed. Every seam failure of its own degrades to
// a nil handoff — the ordinary turn path then behaves exactly as
// before; the checker never introduces a new failure mode.
func (a *UserAgent) prePromptCheck(ctx context.Context, item *queuedItem) *prePromptHandoff {
	// Unbound seams — unit-test constructions and half-wired lanes —
	// skip the checker; the turn's own guards own their failures.
	if a.resolveModel == nil || a.getAgentThread == nil || a.journal == nil {
		return nil
	}
	cfg, err := a.resolveModel(ctx, item.model)
	if err != nil {
		a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
			Msg("pre-prompt check: resolve model; prompt runs unchecked")
		return nil
	}
	records, err := a.getAgentThread(ctx, a.state.agentID)
	if err != nil {
		a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
			Msg("pre-prompt check: load thread; prompt runs unchecked")
		return nil
	}
	// The persona mirrors the turn's block: a missing config or a
	// failed read fails open — a persona-less projection beats none.
	instruction := ""
	if a.getAgentConfig != nil {
		cfg2, err := a.getAgentConfig(ctx, a.state.agentID)
		if err != nil {
			a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
				Msg("pre-prompt check: fetch agent config; measuring without persona")
		} else {
			instruction = BuildSystemPrompt(cfg2)
		}
	}
	var tools backend.TurnTools
	if a.tools != nil {
		resolved, err := a.tools(ctx, a.state.agentID)
		if err != nil {
			a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
				Msg("pre-prompt check: resolve tools; prompt runs unchecked")
			return nil
		}
		tools = resolved
	}
	handoff := &prePromptHandoff{
		item:        item,
		records:     records,
		cfg:         cfg,
		instruction: instruction,
		tools:       tools,
	}
	window := cfg.ContextWindowTokens
	if window <= 0 {
		// Resolution answered zero — nothing declared, learned,
		// cataloged, or stated: skip automation entirely; the prompt
		// still runs.
		a.logger.Warn().Str("model", cfg.Model).Str("agent_id", a.state.agentID).
			Msg("pre-prompt check: no known context window; automation skipped")
		return handoff
	}
	for {
		if turnStopped(ctx) {
			// Stop semantics: the claim that follows runs the prompt
			// with no automation.
			return nil
		}
		whole, toolMass := prePromptTokens(instruction, tools.Infos, records, item.content)
		if len(records) > 0 && whole*100 >= prePromptCompactThreshold*window {
			// Compact decides first, unconditionally: no preceding
			// shake — the summary discards what a shake would reclaim.
			a.setCompacting(true)
			summary, err := a.summarizeForCompaction(ctx, cfg, records, tools.Scrubber)
			a.setCompacting(false)
			if err != nil {
				// Degrade: an unrequested maintenance step must not
				// kill a submitted prompt.
				a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
					Msg("pre-prompt check: summarization failed; prompt proceeds uncompacted")
				break
			}
			if err := a.state.commitAndPublish(func() (*backend.JournalBatch, error) {
				_, batch, err := a.journal.RotateThreadCompacted(ctx, a.state.agentID, item.id, summary)
				return batch, err
			}); err != nil {
				a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
					Msg("pre-prompt check: commit rotation; prompt proceeds uncompacted")
				break
			}
			// The successor's context is the summary; the recall
			// re-reads it and exits far below every trigger.
			reloaded, err := a.getAgentThread(ctx, a.state.agentID)
			if err != nil {
				a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
					Msg("pre-prompt check: reload thread; prompt proceeds")
				break
			}
			records = reloaded
			handoff.records = reloaded
			continue
		}
		if toolMass*100 > prePromptShakeToolThreshold*window {
			// Direct call — zero events, nothing to publish.
			elided, err := a.journal.ShakeAgentContext(ctx, a.state.agentID)
			if err != nil {
				a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
					Msg("pre-prompt check: shake failed; prompt proceeds")
				break
			}
			if elided == 0 {
				// Nothing elidable (protected newest-turn rows): the
				// silent exit — proceed without a sound.
				break
			}
			reloaded, err := a.getAgentThread(ctx, a.state.agentID)
			if err != nil {
				a.logger.Warn().Str("error", a.scrubCopy(err.Error())).Str("agent_id", a.state.agentID).
					Msg("pre-prompt check: reload thread; prompt proceeds")
				break
			}
			records = reloaded
			handoff.records = reloaded
			continue
		}
		break
	}
	if turnStopped(ctx) {
		return nil
	}
	return handoff
}

// runPrePromptCheck brackets one checker run: its context is minted
// from the lane's root and registered for interruption, and the
// compacting flag is unconditionally retired on exit so a degrade can
// never leave it stuck.
func (a *UserAgent) runPrePromptCheck(item *queuedItem) *prePromptHandoff {
	ctx, cancel := context.WithCancel(a.state.rootCtx)
	a.state.setChecking(item, cancel)
	defer func() {
		cancel()
		a.state.clearChecking()
		a.setCompacting(false)
	}()
	return a.prePromptCheck(ctx, item)
}

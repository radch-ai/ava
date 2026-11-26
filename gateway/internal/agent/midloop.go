package agent

// The mid-loop compaction middleware: between two completed rounds of
// one running prompt, it measures the next request against the
// resolved window and — at the threshold — summarizes the selected
// range with the shared engine and rotates the thread under the
// running item. The checkpoint runs inside the model node, after every
// outcome of the last round is durable and before the next request:
// the hook's synchronous contract. Failure before commit degrades to
// the unchanged context; cancellation aborts the turn through its
// normal stop path. One operation per checkpoint — no same-checkpoint
// re-estimation after a rotation.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

// midLoopCompactThreshold is the whole-context share of the resolved
// window at which the mid-loop rotation runs. The remaining share is
// the common policy headroom for both baseline sources — no extra
// reserve is added to either side of the comparison.
const midLoopCompactThreshold = 80 // percent of the resolved window

// midLoopCompactor is one running prompt's checkpoint. Everything it
// needs was resolved at turn setup — no per-checkpoint refetch,
// re-resolution, or history re-read.
type midLoopCompactor struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.Message]
	lane    *UserAgent
	itemID  string
	cfg     backend.ResolvedModel
	emitCtx context.Context
	scrub   *backend.Scrubber

	// instruction and toolInfos are the turn's own framing terms: the
	// estimator mirrors the request shape the provider measured.
	instruction string
	toolInfos   []*schema.ToolInfo

	// olderRecords is the pre-prompt boundary captured at turn setup —
	// the ordinary path's summary range, reused without a refetch.
	olderRecords []backend.AgentThreadMessage
	// olderIsSummaryOnly reports a boundary that is already only a
	// seeded compaction summary: rolling fallback applies.
	olderIsSummaryOnly bool
	// olderPrefixCount is the message count ahead of the prompt at
	// turn start; the boundary the context replacement cuts at.
	olderPrefixCount int
}

// BeforeModelRewriteState runs at the checkpoint: measure, maybe
// summarize and rotate, and replace the in-memory context so the next
// request of the SAME item continues in the successor.
func (m *midLoopCompactor) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.Message],
	_ *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.TypedChatModelAgentState[*schema.Message], error) {
	if m == nil || m.lane == nil || state == nil || m.lane.journal == nil {
		return ctx, state, nil
	}
	// Unknown capacity skips the percentage policy — no invented
	// window. No completed round yet means the pre-prompt check
	// owns this boundary (or there is nothing to measure).
	window := m.cfg.ContextWindowTokens
	if window <= 0 || len(state.Messages) <= m.olderPrefixCount+1 {
		return ctx, state, nil
	}

	// The baseline is the latest completed main-model response's
	// reported usage; the estimator substitutes only when the provider
	// reported none. The delta is what was appended after that
	// response — the new tool results, counted exactly once.
	last := -1
	for i := len(state.Messages) - 1; i >= 0; i-- {
		if state.Messages[i] != nil && state.Messages[i].Role == schema.Assistant {
			last = i
			break
		}
	}
	if last < 0 {
		return ctx, state, nil
	}
	baseline := 0
	if usage := state.Messages[last].ResponseMeta; usage != nil && usage.Usage != nil &&
		usage.Usage.TotalTokens > 0 {
		baseline = usage.Usage.TotalTokens
	} else {
		baseline = estimateWireTokens(m.instruction, m.toolInfos, state.Messages[:last+1])
	}
	delta := estimateWireTokens("", nil, state.Messages[last+1:])
	if (baseline+delta)*100 < window*midLoopCompactThreshold {
		return ctx, state, nil
	}

	// Range selection: the saved older prefix when one exists beyond a
	// seeded summary; rolling fallback otherwise.
	var (
		selected []backend.AgentThreadMessage
		retain   backend.MidTurnRetainMode
	)
	ordinary := !m.olderIsSummaryOnly && len(m.olderRecords) > 0
	cut := m.olderPrefixCount
	if ordinary {
		selected = m.olderRecords
		retain = backend.RetainCurrentTurn
	} else {
		// The latest native batch: the last assistant message that
		// declared calls, plus everything after it. The checkpoint's
		// round shape guarantees it exists.
		b := -1
		for i := len(state.Messages) - 1; i >= 0; i-- {
			msg := state.Messages[i]
			if msg != nil && msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
				b = i
				break
			}
		}
		if b < 0 {
			return ctx, state, nil
		}
		// Never rewrite an unchanged summary with no new material
		// beyond it — the selected range must outgrow the boundary.
		if b <= m.olderPrefixCount {
			return ctx, state, nil
		}
		selected = messagesToRecords(state.Messages[:b])
		retain = backend.RetainLatestBatch
		cut = b
	}

	// The summarization runs outside the storage transaction and the
	// publication lock, under the compacting flag the lane already
	// broadcasts. The defer mirrors the pre-prompt checker's bracket:
	// any exit — degrade, stop, or success — clears the flag.
	m.lane.setCompacting(true)
	defer m.lane.setCompacting(false)
	summary, err := m.lane.summarizeForCompaction(ctx, m.cfg, selected, m.scrub)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, errSummaryStopped) {
			// Cancellation never degrades into more work: the turn
			// settles through its stop path.
			return ctx, state, ctx.Err()
		}
		m.lane.logger.Error().Str("error", m.lane.scrubCopy(err.Error())).
			Str("agent_id", m.lane.state.agentID).Msg("mid-loop compaction degraded; turn continues unchanged")
		return ctx, state, nil
	}

	// The rotation commits under the publication boundary, on the
	// emit context: a stop racing the transaction lands as
	// stop-after-commit, never a half-state.
	if err := m.lane.state.commitAndPublish(func() (*backend.JournalBatch, error) {
		_, batch, err := m.lane.journal.RotateThreadMidTurn(
			m.emitCtx, m.lane.state.agentID, m.itemID, summary, retain)
		if err != nil {
			return nil, err
		}
		// Inside the publication boundary: dropping the snapshot's
		// user-row id before the batch publishes means no sync_ready
		// can observe the stale id after the refresh event exists.
		m.lane.noteMidLoopRotation()
		return batch, nil
	}); err != nil {
		if turnStopped(ctx) {
			return ctx, state, ctx.Err()
		}
		m.lane.logger.Error().Str("error", m.lane.scrubCopy(err.Error())).
			Str("agent_id", m.lane.state.agentID).Msg("mid-loop rotation failed; turn continues unchanged")
		return ctx, state, nil
	}

	// Replace the in-memory context: the seeded summary ahead of the
	// retained tail (the prompt and its rounds for ordinary, the
	// latest batch for rolling). The framework persists this state;
	// the next request of the same item runs against it.
	//
	// The seed's wire role: every model request must carry a
	// user-role message. Ordinary keeps the retained prompt, so its
	// assistant frame already does; rolling summarizes the prompt
	// itself, and its continuation would otherwise go out with none —
	// message-shape-strict providers (z.ai's coding endpoint among
	// them) reject such lists outright. The rolling checkpoint rides
	// as a user-side frame; the durable seeded row stays
	// assistant-role for display and replay.
	seed := schema.AssistantMessage(summary, nil)
	if !ordinary {
		seed = schema.UserMessage(summary)
	}
	kept := make([]*schema.Message, 0, len(state.Messages)-cut+1)
	kept = append(kept, seed)
	kept = append(kept, state.Messages[cut:]...)
	state.Messages = kept
	// The boundary is now the seeded summary alone: a later checkpoint
	// of this turn takes rolling fallback against it.
	m.olderPrefixCount = 1
	m.olderIsSummaryOnly = true
	m.olderRecords = nil
	return ctx, state, nil
}

// estimateWireTokens measures one request slice the way the pre-prompt
// estimator frames a whole context: instruction, tool definitions,
// per-message overhead, message content, and the assistant call
// groups' name+arguments — the same visible input/output boundary a
// provider's total usage covers. Exposed reasoning rides the wire with
// the assistant's content, so it counts too; the durable records the
// pre-prompt estimator measures never carry any. Measuring a delta passes
// an empty instruction and no tools.
func estimateWireTokens(instruction string, infos []*schema.ToolInfo, msgs []*schema.Message) int {
	total := estimateTokens(instruction)
	for _, info := range infos {
		raw, _ := json.Marshal(info)
		total += estimateTokens(string(raw))
	}
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		text := msg.Content
		if msg.Role == schema.Tool {
			text = msg.ToolName + msg.Content
		}
		if msg.Role == schema.Assistant {
			text += msg.ReasoningContent
			for _, tc := range msg.ToolCalls {
				text += tc.Function.Name + tc.Function.Arguments
			}
		}
		total += estimateTokens(text) + perMessageOverheadTokens
	}
	return total
}

// messagesToRecords is mapAgentThread's inverse: the summarizer's
// selected range as thread records. Row ids stay empty — the
// transcript renders role, content, and tool names only.
func messagesToRecords(msgs []*schema.Message) []backend.AgentThreadMessage {
	out := make([]backend.AgentThreadMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.User:
			out = append(out, backend.AgentThreadMessage{Role: apitypes.ThreadRoleUser, Content: msg.Content})
		case schema.Assistant:
			rec := backend.AgentThreadMessage{Role: apitypes.ThreadRoleAssistant, Content: msg.Content}
			for _, tc := range msg.ToolCalls {
				rec.ToolCalls = append(rec.ToolCalls, backend.AgentThreadCall{
					ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
				})
			}
			out = append(out, rec)
		case schema.Tool:
			out = append(out, backend.AgentThreadMessage{
				Role: apitypes.ThreadRoleTool, Content: msg.Content,
				ToolCallID: msg.ToolCallID, ToolName: msg.ToolName,
			})
		}
	}
	return out
}

package backend

// read_tool_output: the tool /shake's placeholders name. One read of
// the stored original by row reference. An internal tool, not a
// catalog entry: AgentTools offers it beside the effective ids for
// every agent, so no catalog or grant state can strand a placeholder.
// Hand-rolled rather than eino's typed wrappers because they answer a
// malformed argument with an error, which fails the whole turn; here
// every failure is the tool's own result text, so the model can
// correct its call and the outcome row still records why.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// readToolOutputInfo is read_tool_output's static description.
var readToolOutputInfo = &schema.ToolInfo{
	Name: "read_tool_output",
	Desc: "Restores a tool result that /shake elided from the conversation. " +
		"Pass the reference string exactly as it appears in the elided placeholder.",
	ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"reference": {
			Type:     schema.String,
			Desc:     "reference id from the elided placeholder",
			Required: true,
		},
	}),
}

// readToolOutputInfoJSON is the internal definition's encoded size,
// counted into the advertised budget like any catalog tool's.
var readToolOutputInfoJSON, _ = json.Marshal(readToolOutputInfo)

// readToolOutputTool restores one elided tool result by reference,
// scoped to one agent.
type readToolOutputTool struct {
	svc     *TurnService
	agentID string
}

// Info answers the static description.
func (t *readToolOutputTool) Info(context.Context) (*schema.ToolInfo, error) {
	return readToolOutputInfo, nil
}

// InvokableRun answers the model's call. A malformed argument is
// returned as the result text, not a turn error: the model can correct
// its own call, and the failure is captured as an ordinary outcome.
func (t *readToolOutputTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	var call struct {
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal([]byte(args), &call); err != nil {
		return fmt.Sprintf("read_tool_output: decode arguments: %v", err), nil
	}
	if call.Reference == "" {
		return `read_tool_output: "reference" is required`, nil
	}
	text, err := t.svc.ReadToolOutput(ctx, t.agentID, call.Reference)
	if err != nil {
		// A reference that reads as unknown — or a failed read — is
		// answered as the tool's result: the model sees why the
		// restore did not happen and can correct the call.
		return fmt.Sprintf("read_tool_output: %v", err), nil
	}
	return text, nil
}

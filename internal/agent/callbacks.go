package agent

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	einocallbacks "github.com/cloudwego/eino/utils/callbacks"
)

type WSConn interface {
	WriteJSON(v interface{}) error
}

type ProgressEvent struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type ProgressCallbacks struct {
	*einocallbacks.HandlerHelper
	conn             WSConn
	ChatModelHandler *einocallbacks.ModelCallbackHandler
	ToolHandler      *einocallbacks.ToolCallbackHandler
}

func BuildProgressHandler(conn WSConn) *ProgressCallbacks {
	pc := &ProgressCallbacks{
		HandlerHelper: einocallbacks.NewHandlerHelper(),
		conn:          conn,
	}

	pc.ChatModelHandler = &einocallbacks.ModelCallbackHandler{
		OnStart: func(ctx context.Context, runInfo *callbacks.RunInfo, input *model.CallbackInput) context.Context {
			if pc.conn != nil {
				_ = pc.conn.WriteJSON(ProgressEvent{Type: "thinking"})
			}
			return ctx
		},
	}

	pc.ToolHandler = &einocallbacks.ToolCallbackHandler{
		OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
			if pc.conn != nil {
				_ = pc.conn.WriteJSON(ProgressEvent{
					Type: "tool_start",
					Payload: map[string]interface{}{
						"tool_name": info.Name,
						"args":      input.ArgumentsInJSON,
					},
				})
			}
			return ctx
		},
		OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
			if pc.conn != nil {
				result := output.Response
				_ = pc.conn.WriteJSON(ProgressEvent{
					Type: "tool_end",
					Payload: map[string]interface{}{
						"tool_name": info.Name,
						"result":    result,
					},
				})
			}
			return ctx
		},
	}

	pc.ChatModel(pc.ChatModelHandler)
	pc.Tool(pc.ToolHandler)

	return pc
}

func (pc *ProgressCallbacks) ToHandler() callbacks.Handler {
	return pc.Handler()
}

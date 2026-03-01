package llm

import (
	"context"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type QuotaSession struct {
	UserID  string
	AgentID string
}

type QuotaMiddleware interface {
	BeforeLLM(ctx context.Context, session *QuotaSession) error
	AfterLLM(ctx context.Context, session *QuotaSession, promptTokens, completionTokens int)
}

type QuotaChatModel struct {
	inner   model.ToolCallingChatModel
	quota   QuotaMiddleware
	session *QuotaSession
}

func NewQuotaChatModel(inner model.ToolCallingChatModel, quota QuotaMiddleware, session *QuotaSession) *QuotaChatModel {
	return &QuotaChatModel{
		inner:   inner,
		quota:   quota,
		session: session,
	}
}

func (q *QuotaChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if err := q.quota.BeforeLLM(ctx, q.session); err != nil {
		return nil, err
	}

	result, err := q.inner.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	if result.ResponseMeta != nil && result.ResponseMeta.Usage != nil {
		usage := result.ResponseMeta.Usage
		q.quota.AfterLLM(ctx, q.session, usage.PromptTokens, usage.CompletionTokens)
	}

	return result, nil
}

func (q *QuotaChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := q.quota.BeforeLLM(ctx, q.session); err != nil {
		return nil, err
	}

	stream, err := q.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	return q.wrapStreamForQuota(ctx, stream), nil
}

func (q *QuotaChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	innerWithTools, err := q.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}

	return &QuotaChatModel{
		inner:   innerWithTools,
		quota:   q.quota,
		session: q.session,
	}, nil
}

func (q *QuotaChatModel) GetInnerModel() model.ToolCallingChatModel {
	return q.inner
}

func (q *QuotaChatModel) wrapStreamForQuota(ctx context.Context, stream *schema.StreamReader[*schema.Message]) *schema.StreamReader[*schema.Message] {
	sr, sw := schema.Pipe[*schema.Message](10)

	go func() {
		defer sw.Close()

		var lastMessage *schema.Message

		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				sw.Send(msg, err)
				break
			}

			lastMessage = msg
			sw.Send(msg, nil)
		}

		if lastMessage != nil && lastMessage.ResponseMeta != nil && lastMessage.ResponseMeta.Usage != nil {
			usage := lastMessage.ResponseMeta.Usage
			q.quota.AfterLLM(ctx, q.session, usage.PromptTokens, usage.CompletionTokens)
		}
	}()

	return sr
}

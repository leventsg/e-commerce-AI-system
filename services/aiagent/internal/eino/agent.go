package eino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

type RunRequest struct {
	ConversationID string
	MessageID      string
	Messages       []domain.ContextMessage
}

type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
}

type runner struct {
	model model.BaseChatModel
}

func NewRunner(chatModel model.BaseChatModel) Runner {
	return &runner{model: chatModel}
}

func (r *runner) Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error) {
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	response, err := r.model.Generate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return nil, ErrEmptyModelResponse
	}
	return []domain.AgentEvent{assistantEvent(req, response.Content, true)}, nil
}

func (r *runner) Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error) {
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	stream, err := r.model.Stream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}

	out := make(chan domain.AgentEvent)
	go func() {
		defer close(out)
		defer stream.Close()
		for {
			chunk, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				return
			}
			if recvErr != nil {
				logx.WithContext(ctx).Errorw("ai chat model stream failed", logx.Field("component", "chat_model"), logx.Field("stage", "stream"), logx.Field("reason", ErrorReason(recvErr)), logx.Field("err", recvErr))
				out <- domain.AgentEvent{
					Type:           domain.EventError,
					ConversationID: req.ConversationID,
					MessageID:      req.MessageID,
					Content:        ErrModelUnavailable.Error(),
					Done:           true,
				}
				return
			}
			if chunk == nil {
				continue
			}
			out <- assistantEvent(req, chunk.Content, true)
		}
	}()
	return out, nil
}

func buildInputMessages(req RunRequest) ([]*schema.Message, error) {
	return ConvertContextMessages(req.Messages)
}

func assistantEvent(req RunRequest, content string, done bool) domain.AgentEvent {
	return domain.AgentEvent{
		Type:           domain.EventAssistantMessage,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Content:        content,
		Done:           done,
	}
}

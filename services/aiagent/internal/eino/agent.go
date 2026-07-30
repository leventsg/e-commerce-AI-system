package eino

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/zeromicro/go-zero/core/logx"
)

const defaultReActMaxStep = 8

type RunRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ClientIP       string
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

type reActRunner struct {
	model model.ToolCallingChatModel
	tools []einotool.InvokableTool
}

func NewReActRunner(chatModel model.ToolCallingChatModel, tools []einotool.InvokableTool) Runner {
	return &reActRunner{model: chatModel, tools: tools}
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

func (r *reActRunner) Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error) {
	if r == nil || r.model == nil {
		return nil, ErrModelUnavailable
	}
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: r.model,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: toBaseTools(r.tools),
		},
		MaxStep: defaultReActMaxStep,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	ctx = aitools.WithToolExecutionContext(ctx, aitools.ToolExecutionContext{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		ClientIP:       req.ClientIP,
	})
	response, err := agent.Generate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return nil, ErrEmptyModelResponse
	}
	return []domain.AgentEvent{assistantEvent(req, response.Content, true)}, nil
}

func (r *reActRunner) Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error) {
	events, err := r.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan domain.AgentEvent, len(events))
	for _, event := range events {
		out <- event
	}
	close(out)
	return out, nil
}

func toBaseTools(tools []einotool.InvokableTool) []einotool.BaseTool {
	result := make([]einotool.BaseTool, 0, len(tools))
	for _, item := range tools {
		if item != nil {
			result = append(result, item)
		}
	}
	return result
}

package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

const defaultAgentMaxIterations = 8

type RunRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ClientIP       string
	Messages       []domain.ContextMessage
	OnEvent        func(context.Context, domain.AgentEvent) error
}

type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
}

type agent struct {
	model model.ToolCallingChatModel
	tools []einotool.InvokableTool
}

func NewAgent(chatModel model.ToolCallingChatModel, tools []einotool.InvokableTool) Runner {
	return &agent{model: chatModel, tools: tools}
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

func (r *agent) Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error) {
	if r == nil || r.model == nil {
		return nil, ErrModelUnavailable
	}
	input, err := buildInputMessages(req)
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "ai_customer_service",
		Description: "E-commerce customer service agent",
		Model:       r.model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: invokableToolsToBaseTools(r.tools),
			},
		},
		MaxIterations: defaultAgentMaxIterations,
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
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx, input)
	events := make([]domain.AgentEvent, 0, 2)
	hasAssistant := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, event.Err)
		}
		domainEvent, ok, err := adkEventToDomainEvent(event, req)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if domainEvent.Type == domain.EventAssistantMessage {
			hasAssistant = true
		}
		if req.OnEvent != nil {
			if err := req.OnEvent(ctx, domainEvent); err != nil {
				return nil, err
			}
		}
		events = append(events, domainEvent)
	}
	if !hasAssistant {
		return nil, ErrEmptyModelResponse
	}
	return events, nil
}

func (r *agent) Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error) {
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

func invokableToolsToBaseTools(tools []einotool.InvokableTool) []einotool.BaseTool {
	result := make([]einotool.BaseTool, 0, len(tools))
	for _, item := range tools {
		if item != nil {
			result = append(result, item)
		}
	}
	return result
}

func adkEventToDomainEvent(event *adk.AgentEvent, req RunRequest) (domain.AgentEvent, bool, error) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
		return domain.AgentEvent{}, false, nil
	}
	message, _, err := adk.GetMessage(event)
	if err != nil {
		return domain.AgentEvent{}, false, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return domain.AgentEvent{}, false, nil
	}
	output := event.Output.MessageOutput
	switch output.Role {
	case schema.Assistant:
		return domain.AgentEvent{
			Type:           domain.EventAssistantMessage,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			Content:        message.Content,
			Done:           true,
		}, true, nil
	case schema.Tool:
		toolName := output.ToolName
		if toolName == "" {
			toolName = message.ToolName
		}
		return domain.AgentEvent{
			Type:           domain.EventToolResult,
			ConversationID: req.ConversationID,
			MessageID:      newAgentMessageID(),
			ToolCallID:     message.ToolCallID,
			Content:        message.Content,
			Tool:           toolName,
			Status:         "success",
			DataJSON:       message.Content,
			Done:           true,
		}, true, nil
	default:
		return domain.AgentEvent{}, false, nil
	}
}

func newAgentMessageID() string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return "msg_" + id.String()
}

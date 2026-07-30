package eino

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

type emptyResponseModel struct{ response *schema.Message }

func (m emptyResponseModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.response, nil
}
func (m emptyResponseModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

func TestRunnerRejectsNilAndEmptyModelResponses(t *testing.T) {
	for _, response := range []*schema.Message{nil, schema.AssistantMessage("", nil)} {
		_, err := NewRunner(emptyResponseModel{response: response}).Run(context.Background(), RunRequest{
			ConversationID: "conv-1",
			Messages:       []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "hello"}},
		})
		if !errors.Is(err, ErrEmptyModelResponse) {
			t.Fatalf("Run() error = %v, want ErrEmptyModelResponse", err)
		}
	}
}

func TestRunnerConsumesPrebuiltContextMessages(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage("你好", nil)}
	contextMessages := []domain.ContextMessage{
		{Role: domain.ContextRoleSystem, Content: "安全指令"},
		{Role: domain.ContextRoleAssistant, Content: "[conversation_summary]\n旧摘要"},
		{Role: domain.ContextRoleTool, Content: `{"cart_item_id":7}`, ToolCallID: "call-1", ToolName: "cart.list"},
		{Role: domain.ContextRoleUser, Content: "继续"},
	}

	events, err := NewRunner(chatModel).Run(context.Background(), RunRequest{
		ConversationID: "conv-1", MessageID: "msg-1", Messages: contextMessages,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].Content != "你好" {
		t.Fatalf("events = %+v", events)
	}
	if len(chatModel.messages) != len(contextMessages) {
		t.Fatalf("model message count = %d, want %d", len(chatModel.messages), len(contextMessages))
	}
	if chatModel.messages[2].Role != schema.Tool ||
		chatModel.messages[2].ToolCallID != "call-1" ||
		chatModel.messages[2].ToolName != "cart.list" {
		t.Fatalf("tool message = %+v", chatModel.messages[2])
	}
}

func TestReActRunnerExecutesToolCallAndReturnsFinalAssistantMessage(t *testing.T) {
	chatModel := &capturingToolCallingChatModel{
		responses: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: schema.FunctionCall{
					Name:      "cart.list",
					Arguments: `{"page":1}`,
				},
			}}),
			schema.AssistantMessage("购物车里有 1 件商品", nil),
		},
	}
	tool := &capturingInvokableTool{
		info:   &schema.ToolInfo{Name: "cart.list", Desc: "list cart"},
		result: `{"items":[{"product_id":12}]}`,
	}

	events, err := NewReActRunner(chatModel, []einotool.InvokableTool{tool}).Run(context.Background(), RunRequest{
		UserID:         42,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		ClientIP:       "127.0.0.1",
		Messages:       []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "查购物车"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(events) != 1 || events[0].Content != "购物车里有 1 件商品" || events[0].MessageID != "msg-1" {
		t.Fatalf("events = %+v", events)
	}
	if tool.calls != 1 || tool.arguments != `{"page":1}` {
		t.Fatalf("tool calls=%d args=%q", tool.calls, tool.arguments)
	}
	execution, ok := tool.execution.(aitools.ToolExecutionContext)
	if !ok || execution.UserID != 42 || execution.ConversationID != "conv-1" || execution.MessageID != "msg-1" || execution.ClientIP != "127.0.0.1" {
		t.Fatalf("tool execution context = %+v", tool.execution)
	}
	if len(chatModel.messages) < 3 || chatModel.messages[1].Role != schema.Assistant || chatModel.messages[2].Role != schema.Tool {
		t.Fatalf("react messages = %+v", chatModel.messages)
	}
}

type capturingChatModel struct {
	response *schema.Message
	messages []*schema.Message
}

func (m *capturingChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.messages = messages
	return m.response, nil
}

func (m *capturingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingToolCallingChatModel struct {
	capturingChatModel
	responses  []*schema.Message
	boundTools []*schema.ToolInfo
}

func (m *capturingToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.boundTools = tools
	return m, nil
}

func (m *capturingToolCallingChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.messages = append([]*schema.Message(nil), messages...)
	if len(m.responses) > 0 {
		response := m.responses[0]
		m.responses = m.responses[1:]
		return response, nil
	}
	return m.capturingChatModel.Generate(ctx, messages, opts...)
}

func (m *capturingToolCallingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

type capturingInvokableTool struct {
	info      *schema.ToolInfo
	result    string
	calls     int
	arguments string
	execution any
}

func (t *capturingInvokableTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *capturingInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	t.calls++
	t.arguments = arguments
	execution, _ := aitools.ToolExecutionFromContext(ctx)
	t.execution = execution
	return t.result, nil
}

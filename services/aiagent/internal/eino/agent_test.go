package eino

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
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

package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestIntentModelAdapterConvertsDomainMessagesAtEinoBoundary(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage(`{"intent":"chat"}`, nil)}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))

	intentModel, err := NewIntentModelFactory(factory).NewIntentModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"})
	if err != nil {
		t.Fatalf("NewIntentModel() error = %v", err)
	}
	content, err := intentModel.Generate(context.Background(), []domain.ContextMessage{
		{Role: domain.ContextRoleSystem, Content: "intent system"},
		{Role: domain.ContextRoleUser, Content: "你好"},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if content != `{"intent":"chat"}` || len(chatModel.messages) != 2 || chatModel.messages[0].Role != schema.System {
		t.Fatalf("content=%q messages=%+v", content, chatModel.messages)
	}
}

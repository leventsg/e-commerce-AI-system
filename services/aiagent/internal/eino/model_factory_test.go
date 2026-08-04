package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

func TestModelFactoryCreatesStructuredChatModelWithJSONObject(t *testing.T) {
	structured := StructuredOutputConfig{
		Name:        "person",
		Description: "data that describes a person",
	}
	var captured StructuredOutputConfig
	var provider string
	chatModel := &capturingChatModel{response: schema.AssistantMessage(`{"name":"John"}`, nil)}
	factory := NewModelFactory(WithStructuredChatModelBuilder(func(_ context.Context, gotProvider string, _ config.EinoConfig, gotStructured StructuredOutputConfig) (model.BaseChatModel, error) {
		provider = gotProvider
		captured = gotStructured
		return chatModel, nil
	}))

	got, err := factory.NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, structured)
	if err != nil {
		t.Fatalf("NewStructuredChatModel() error = %v", err)
	}
	if got != chatModel || provider != "openai-compatible" {
		t.Fatalf("model=%T provider=%q", got, provider)
	}
	if captured.Name != "person" || captured.Description != "data that describes a person" {
		t.Fatalf("structured config = %+v", captured)
	}
}

func TestModelFactoryRejectsInvalidStructuredConfig(t *testing.T) {
	factory := NewModelFactory()
	_, err := factory.NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, StructuredOutputConfig{})
	if err == nil {
		t.Fatal("NewStructuredChatModel() error = nil, want validation error")
	}
}

func TestModelFactoryPlainChatModelDoesNotRequireStructuredConfig(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage("ok", nil)}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))

	got, err := factory.NewChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"})
	if err != nil {
		t.Fatalf("NewChatModel() error = %v", err)
	}
	if got != chatModel {
		t.Fatalf("model=%T, want capturingChatModel", got)
	}
}

func TestModelFactoryBindsToolsWhenProvided(t *testing.T) {
	toolInfo := &schema.ToolInfo{Name: "cart_list", Desc: "list cart"}
	chatModel := &capturingToolCallingChatModel{capturingChatModel: capturingChatModel{response: schema.AssistantMessage("ok", nil)}}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))

	got, err := factory.NewChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, toolInfo)
	if err != nil {
		t.Fatalf("NewChatModel() error = %v", err)
	}
	if got != chatModel || len(chatModel.boundTools) != 1 || chatModel.boundTools[0] != toolInfo {
		t.Fatalf("model=%T boundTools=%+v", got, chatModel.boundTools)
	}
}

func TestModelFactoryRejectsToolsForNonToolCallingModel(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage("ok", nil)}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))

	_, err := factory.NewChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, &schema.ToolInfo{Name: "cart_list"})
	if err == nil || !strings.Contains(err.Error(), "tool calling") {
		t.Fatalf("NewChatModel() error = %v, want tool calling error", err)
	}
}

func TestModelFactoryBindsToolsForStructuredModelWhenProvided(t *testing.T) {
	toolInfo := &schema.ToolInfo{Name: "cart_list", Desc: "list cart"}
	chatModel := &capturingToolCallingChatModel{capturingChatModel: capturingChatModel{response: schema.AssistantMessage(`{"ok":true}`, nil)}}
	factory := NewModelFactory(WithStructuredChatModelBuilder(func(_ context.Context, _ string, _ config.EinoConfig, _ StructuredOutputConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))

	got, err := factory.NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, StructuredOutputConfig{
		Name: "person",
	}, toolInfo)
	if err != nil {
		t.Fatalf("NewStructuredChatModel() error = %v", err)
	}
	if got != chatModel || len(chatModel.boundTools) != 1 || chatModel.boundTools[0] != toolInfo {
		t.Fatalf("model=%T boundTools=%+v", got, chatModel.boundTools)
	}
}

func TestOpenAICompatibleConfigSetsJSONObjectResponseFormat(t *testing.T) {
	openAIConfig := buildOpenAICompatibleModelConfig(config.EinoConfig{
		APIKey: "key", BaseURL: "https://example.com", Model: "fast", MaxTokens: 512, Temperature: 0.2,
	}, &StructuredOutputConfig{
		Name:        "person",
		Description: "data that describes a person",
	})

	if openAIConfig.ResponseFormat == nil || openAIConfig.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONObject {
		t.Fatalf("response format = %+v", openAIConfig.ResponseFormat)
	}
	if openAIConfig.ResponseFormat.JSONSchema != nil {
		t.Fatalf("json schema response format = %+v, want nil", openAIConfig.ResponseFormat.JSONSchema)
	}
}

func TestModelFactoryRejectsMissingModelForStructuredChatModel(t *testing.T) {
	_, err := NewModelFactory().NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek"}, StructuredOutputConfig{
		Name: "person",
	})
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("NewStructuredChatModel() error = %v", err)
	}
}

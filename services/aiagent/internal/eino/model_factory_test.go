package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

func TestModelFactoryCreatesStructuredChatModelWithJSONSchema(t *testing.T) {
	js := personJSONSchema()
	structured := StructuredOutputConfig{
		Name:        "person",
		Description: "data that describes a person",
		Strict:      true,
		Schema:      js,
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
	if captured.Name != "person" || captured.Description != "data that describes a person" || !captured.Strict || captured.Schema != js {
		t.Fatalf("structured config = %+v", captured)
	}
}

func TestModelFactoryRejectsInvalidStructuredConfig(t *testing.T) {
	factory := NewModelFactory()
	for _, tc := range []struct {
		name       string
		structured StructuredOutputConfig
	}{
		{name: "empty name", structured: StructuredOutputConfig{Schema: personJSONSchema()}},
		{name: "nil schema", structured: StructuredOutputConfig{Name: "person"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := factory.NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek", Model: "fast"}, tc.structured)
			if err == nil {
				t.Fatal("NewStructuredChatModel() error = nil, want validation error")
			}
		})
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

func TestOpenAICompatibleConfigSetsStructuredResponseFormat(t *testing.T) {
	js := personJSONSchema()
	openAIConfig := buildOpenAICompatibleModelConfig(config.EinoConfig{
		APIKey: "key", BaseURL: "https://example.com", Model: "fast", MaxTokens: 512, Temperature: 0.2,
	}, &StructuredOutputConfig{
		Name:        "person",
		Description: "data that describes a person",
		Strict:      true,
		Schema:      js,
	})

	if openAIConfig.ResponseFormat == nil || openAIConfig.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONSchema {
		t.Fatalf("response format = %+v", openAIConfig.ResponseFormat)
	}
	got := openAIConfig.ResponseFormat.JSONSchema
	if got == nil || got.Name != "person" || got.Description != "data that describes a person" || !got.Strict || got.JSONSchema != js {
		t.Fatalf("json schema response format = %+v", got)
	}
}

func personJSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: string(schema.Object),
		Properties: orderedmap.New[string, *jsonschema.Schema](
			orderedmap.WithInitialData[string, *jsonschema.Schema](
				orderedmap.Pair[string, *jsonschema.Schema]{Key: "name", Value: &jsonschema.Schema{Type: string(schema.String)}},
				orderedmap.Pair[string, *jsonschema.Schema]{Key: "height", Value: &jsonschema.Schema{Type: string(schema.Integer)}},
				orderedmap.Pair[string, *jsonschema.Schema]{Key: "weight", Value: &jsonschema.Schema{Type: string(schema.Integer)}},
			),
		),
	}
}

func TestModelFactoryRejectsMissingModelForStructuredChatModel(t *testing.T) {
	_, err := NewModelFactory().NewStructuredChatModel(context.Background(), config.EinoConfig{Provider: "deepseek"}, StructuredOutputConfig{
		Name:   "person",
		Schema: personJSONSchema(),
	})
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("NewStructuredChatModel() error = %v", err)
	}
}

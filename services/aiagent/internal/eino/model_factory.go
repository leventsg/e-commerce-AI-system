package eino

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

var ErrModelUnavailable = errors.New("ai model unavailable, please retry later")

type ModelFactory interface {
	NewChatModel(ctx context.Context, cfg config.EinoConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error)
	NewStructuredChatModel(ctx context.Context, cfg config.EinoConfig, structured StructuredOutputConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error)
}

type ChatModelBuilder func(ctx context.Context, provider string, cfg config.EinoConfig) (model.BaseChatModel, error)
type StructuredChatModelBuilder func(ctx context.Context, provider string, cfg config.EinoConfig, structured StructuredOutputConfig) (model.BaseChatModel, error)

type StructuredOutputConfig struct {
	Name        string
	Description string
}

type modelFactory struct {
	builder           ChatModelBuilder
	structuredBuilder StructuredChatModelBuilder
}

type ModelFactoryOption func(*modelFactory)

func WithChatModelBuilder(builder ChatModelBuilder) ModelFactoryOption {
	return func(f *modelFactory) {
		f.builder = builder
	}
}

func WithStructuredChatModelBuilder(builder StructuredChatModelBuilder) ModelFactoryOption {
	return func(f *modelFactory) {
		f.structuredBuilder = builder
	}
}

func NewModelFactory(opts ...ModelFactoryOption) ModelFactory {
	f := &modelFactory{
		builder:           buildOpenAICompatibleModel,
		structuredBuilder: buildOpenAICompatibleStructuredModel,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (f *modelFactory) NewChatModel(ctx context.Context, cfg config.EinoConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error) {
	provider, err := normalizeProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("model is required for provider %q", cfg.Provider)
	}

	chatModel, err := f.builder(ctx, provider, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if chatModel == nil {
		return nil, fmt.Errorf("%w: nil chat model", ErrModelUnavailable)
	}
	return bindToolsIfProvided(chatModel, tools)
}

// NewStructuredChatModel 创建结构化输出模型
func (f *modelFactory) NewStructuredChatModel(ctx context.Context, cfg config.EinoConfig, structured StructuredOutputConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error) {
	provider, err := normalizeProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("model is required for provider %q", cfg.Provider)
	}
	if err := validateStructuredOutputConfig(structured); err != nil {
		return nil, err
	}

	chatModel, err := f.structuredBuilder(ctx, provider, cfg, structured)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if chatModel == nil {
		return nil, fmt.Errorf("%w: nil chat model", ErrModelUnavailable)
	}
	return bindToolsIfProvided(chatModel, tools)
}

// bindToolsIfProvided 绑定工具到模型
func bindToolsIfProvided(chatModel model.BaseChatModel, tools []*schema.ToolInfo) (model.BaseChatModel, error) {
	if len(tools) == 0 {
		return chatModel, nil
	}
	toolCallingModel, ok := chatModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("%w: model does not support tool calling", ErrModelUnavailable)
	}
	bound, err := toolCallingModel.WithTools(tools)
	if err != nil {
		return nil, fmt.Errorf("%w: bind tools: %v", ErrModelUnavailable, err)
	}
	if bound == nil {
		return nil, fmt.Errorf("%w: nil tool calling model", ErrModelUnavailable)
	}
	return bound, nil
}

func normalizeProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "":
		return "", errors.New("provider is required")
	case "openai-compatible", "openai":
		return "openai-compatible", nil
	case "deepseek":
		return "openai-compatible", nil
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
}

func buildOpenAICompatibleModel(ctx context.Context, _ string, cfg config.EinoConfig) (model.BaseChatModel, error) {
	return openai.NewChatModel(ctx, buildOpenAICompatibleModelConfig(cfg, nil))
}

func buildOpenAICompatibleStructuredModel(ctx context.Context, _ string, cfg config.EinoConfig, structured StructuredOutputConfig) (model.BaseChatModel, error) {
	return openai.NewChatModel(ctx, buildOpenAICompatibleModelConfig(cfg, &structured))
}

func buildOpenAICompatibleModelConfig(cfg config.EinoConfig, structured *StructuredOutputConfig) *openai.ChatModelConfig {
	temperature := float32(cfg.Temperature)
	timeout := time.Duration(cfg.Timeout) * time.Second
	openAIConfig := &openai.ChatModelConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Timeout:     timeout,
		MaxTokens:   &cfg.MaxTokens,
		Temperature: &temperature,
	}
	if cfg.MaxTokens <= 0 {
		openAIConfig.MaxTokens = nil
	}
	if structured != nil {
		openAIConfig.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}
	return openAIConfig
}

func validateStructuredOutputConfig(structured StructuredOutputConfig) error {
	if strings.TrimSpace(structured.Name) == "" {
		return errors.New("structured output schema name is required")
	}
	return nil
}

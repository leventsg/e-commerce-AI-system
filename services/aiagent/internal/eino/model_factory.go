package eino

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

var ErrModelUnavailable = errors.New("ai model unavailable, please retry later")

type ModelFactory interface {
	NewChatModel(ctx context.Context, cfg config.EinoConfig) (model.BaseChatModel, error)
}

type ChatModelBuilder func(ctx context.Context, provider string, cfg config.EinoConfig) (model.BaseChatModel, error)

type modelFactory struct {
	builder ChatModelBuilder
}

type ModelFactoryOption func(*modelFactory)

func WithChatModelBuilder(builder ChatModelBuilder) ModelFactoryOption {
	return func(f *modelFactory) {
		f.builder = builder
	}
}

func NewModelFactory(opts ...ModelFactoryOption) ModelFactory {
	f := &modelFactory{builder: buildOpenAICompatibleModel}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (f *modelFactory) NewChatModel(ctx context.Context, cfg config.EinoConfig) (model.BaseChatModel, error) {
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
	return chatModel, nil
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
	return openai.NewChatModel(ctx, openAIConfig)
}

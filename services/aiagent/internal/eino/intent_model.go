package eino

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner"
)

type intentModelFactory struct {
	modelFactory ModelFactory
}

func NewIntentModelFactory(factory ModelFactory) planner.IntentModelFactory {
	return &intentModelFactory{modelFactory: factory}
}

func (f *intentModelFactory) NewIntentModel(ctx context.Context, cfg config.EinoConfig) (planner.IntentModel, error) {
	if f == nil || f.modelFactory == nil {
		return nil, ErrModelUnavailable
	}
	chatModel, err := f.modelFactory.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &intentModel{model: chatModel}, nil
}

type intentModel struct {
	model model.BaseChatModel
}

func (m *intentModel) Generate(ctx context.Context, messages []domain.ContextMessage) (string, error) {
	input, err := ConvertContextMessages(messages)
	if err != nil {
		return "", err
	}
	response, err := m.model.Generate(ctx, input)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return "", planner.ErrEmptyIntentModelResponse
	}
	return response.Content, nil
}

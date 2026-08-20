package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	ragprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/rag"
)

type ragClassifier struct {
	modelFactory eino.ModelFactory
	cfg          config.EinoConfig
}

func newRagClassifier(modelFactory eino.ModelFactory, cfg config.EinoConfig) *ragClassifier {
	return &ragClassifier{modelFactory: modelFactory, cfg: cfg}
}

func (c *ragClassifier) Classify(ctx context.Context, input string) (Decision, error) {
	if c == nil || c.modelFactory == nil {
		return Decision{}, fmt.Errorf("rag classifier unavailable")
	}
	cfg := c.cfg
	cfg.Temperature = 0
	if cfg.MaxTokens <= 0 || cfg.MaxTokens > 128 {
		cfg.MaxTokens = 64
	}
	chatModel, err := c.modelFactory.NewChatModel(ctx, cfg)
	if err != nil {
		return Decision{}, err
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(ragprompt.ClassifySystemPrompt),
		schema.UserMessage(input),
	})
	if err != nil {
		return Decision{}, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return Decision{}, fmt.Errorf("rag classifier returned empty response")
	}
	decision, err := parseDecision(response.Content)
	if err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func parseDecision(raw string) (Decision, error) {
	content := strings.TrimSpace(raw)
	if start := strings.Index(content, "{"); start >= 0 {
		if end := strings.LastIndex(content, "}"); end > start {
			content = content[start : end+1]
		}
	}
	var decision Decision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return Decision{}, fmt.Errorf("parse rag classifier output: %w", err)
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return Decision{}, fmt.Errorf("rag classifier confidence out of range: %v", decision.Confidence)
	}
	return decision, nil
}

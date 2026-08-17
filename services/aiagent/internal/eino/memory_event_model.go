package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryeventextractor"
	memoryeventprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/memoryevent"
)

type memoryEventExtractorModel struct {
	modelFactory ModelFactory
	cfg          config.EinoConfig
}

func NewUserMemoryEventExtractorModel(factory ModelFactory, cfg config.EinoConfig) memoryeventextractor.Model {
	return &memoryEventExtractorModel{modelFactory: factory, cfg: cfg}
}

func (m *memoryEventExtractorModel) Extract(ctx context.Context, req memoryeventextractor.ExtractRequest) (memoryeventextractor.Candidate, error) {
	if m == nil || m.modelFactory == nil {
		return memoryeventextractor.Candidate{}, ErrModelUnavailable
	}
	chatModel, err := m.modelFactory.NewStructuredChatModel(ctx, m.cfg, memoryEventStructuredOutputConfig())
	if err != nil {
		return memoryeventextractor.Candidate{}, err
	}
	userPrompt, err := buildMemoryEventUserPrompt(req)
	if err != nil {
		return memoryeventextractor.Candidate{}, err
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(memoryeventprompt.SystemPrompt),
		schema.UserMessage(userPrompt),
	})
	if err != nil {
		return memoryeventextractor.Candidate{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return memoryeventextractor.Candidate{}, memoryeventextractor.ErrRejectedCandidate
	}
	return memoryeventextractor.ParseCandidate(response.Content)
}

func memoryEventStructuredOutputConfig() StructuredOutputConfig {
	return StructuredOutputConfig{
		Name:        "ai_user_memory_events",
		Description: "candidate timeline events extracted from compressed AI customer-service conversation messages",
	}
}

type memoryEventPromptPayload struct {
	Event    memoryeventextractor.UpdateEvent `json:"event"`
	Messages []memoryEventMessagePrompt       `json:"messages"`
}

type memoryEventMessagePrompt struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func buildMemoryEventUserPrompt(req memoryeventextractor.ExtractRequest) (string, error) {
	raw, err := json.Marshal(memoryEventPromptPayload{
		Event:    req.Event,
		Messages: buildMemoryEventMessagePrompts(req.Messages),
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func buildMemoryEventMessagePrompts(messages []*aimessages.AiMessages) []memoryEventMessagePrompt {
	result := make([]memoryEventMessagePrompt, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		result = append(result, memoryEventMessagePrompt{
			ID:        message.MsgId,
			Role:      message.Role,
			Content:   redactMemoryEventSensitiveContext(message.Content),
			CreatedAt: formatMemoryEventTime(message.CreatedAt),
		})
	}
	return result
}

func formatMemoryEventTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

var (
	memoryEventSensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*=\s*[^\s,，;；]+`)
	memoryEventSensitiveColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

func redactMemoryEventSensitiveContext(content string) string {
	content = memoryEventSensitiveAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return memoryEventSensitiveColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

var _ memoryeventextractor.Model = (*memoryEventExtractorModel)(nil)

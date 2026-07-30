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
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	profileprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/profile"
)

type profileExtractorModel struct {
	modelFactory ModelFactory
	cfg          config.EinoConfig
}

func NewProfileExtractorModel(factory ModelFactory, cfg config.EinoConfig) profileextractor.Model {
	return &profileExtractorModel{modelFactory: factory, cfg: cfg}
}

// Extract 从用户消息和上下文中提取用户画像更新候选
func (m *profileExtractorModel) Extract(ctx context.Context, req profileextractor.ExtractRequest) (profileextractor.Candidate, error) {
	if m == nil || m.modelFactory == nil {
		return profileextractor.Candidate{}, ErrModelUnavailable
	}
	chatModel, err := m.modelFactory.NewStructuredChatModel(ctx, m.cfg, profileStructuredOutputConfig())
	if err != nil {
		return profileextractor.Candidate{}, err
	}
	userPrompt, err := buildProfileUserPrompt(req)
	if err != nil {
		return profileextractor.Candidate{}, err
	}
	response, err := chatModel.Generate(ctx, []*schema.Message{
		schema.SystemMessage(profileprompt.SystemPrompt),
		schema.UserMessage(userPrompt),
	})
	if err != nil {
		return profileextractor.Candidate{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return profileextractor.Candidate{}, profileextractor.ErrRejectedCandidate
	}
	return profileextractor.ParseCandidate(response.Content)
}

func profileStructuredOutputConfig() StructuredOutputConfig {
	return StructuredOutputConfig{
		Name:        "ai_user_profile_update",
		Description: "candidate JSON patch for updating an AI chat derived user profile",
	}
}

type profilePromptPayload struct {
	Event           profileextractor.UpdateEvent `json:"event"`
	ExistingProfile json.RawMessage              `json:"existing_profile,omitempty"`
	Memories        []profileMemoryPrompt        `json:"memories,omitempty"`
	Messages        []profileMessagePrompt       `json:"messages"`
}

type profileMemoryPrompt struct {
	Key        string  `json:"key"`
	Type       string  `json:"type,omitempty"`
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence,omitempty"`
	Source     string  `json:"source,omitempty"`
}

type profileMessagePrompt struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func buildProfileUserPrompt(req profileextractor.ExtractRequest) (string, error) {
	payload := profilePromptPayload{
		Event:    req.Event,
		Memories: buildProfileMemoryPrompts(req.Memories),
		Messages: buildProfileMessagePrompts(req.Messages),
	}
	if req.Profile != nil && len(req.Profile.ProfileJSON) > 0 {
		payload.ExistingProfile = req.Profile.ProfileJSON
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func buildProfileMemoryPrompts(memories []domain.UserMemory) []profileMemoryPrompt {
	result := make([]profileMemoryPrompt, 0, len(memories))
	for _, memory := range memories {
		result = append(result, profileMemoryPrompt{
			Key:        memory.Key,
			Type:       memory.Type,
			Content:    redactProfileSensitiveContext(memory.Content),
			Confidence: memory.Confidence,
			Source:     memory.Source,
		})
	}
	return result
}

func buildProfileMessagePrompts(messages []*aimessages.AiMessages) []profileMessagePrompt {
	result := make([]profileMessagePrompt, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		result = append(result, profileMessagePrompt{
			ID:        message.MsgId,
			Role:      message.Role,
			Content:   redactProfileSensitiveContext(message.Content),
			CreatedAt: formatProfileTime(message.CreatedAt),
		})
	}
	return result
}

func formatProfileTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

var (
	profileSensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*=\s*[^\s,，;；]+`)
	profileSensitiveColonPattern      = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth)\b\s*[:：]\s*[^\s,，;；]+`)
)

func redactProfileSensitiveContext(content string) string {
	content = profileSensitiveAssignmentPattern.ReplaceAllString(content, "$1=[redacted]")
	return profileSensitiveColonPattern.ReplaceAllString(content, "$1:[redacted]")
}

var _ profileextractor.Model = (*profileExtractorModel)(nil)

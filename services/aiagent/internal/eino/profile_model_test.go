package eino

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
)

func TestProfileExtractorModelUsesStructuredChatModel(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage(`{"should_update":false,"update_type":"none","profile_patch":{},"evidence_message_ids":[],"confidence":0,"reason":""}`, nil)}
	var captured StructuredOutputConfig
	structuredCalls := 0
	plainCalls := 0
	factory := NewModelFactory(
		WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
			plainCalls++
			return nil, errors.New("plain chat model must not be used for profile extraction")
		}),
		WithStructuredChatModelBuilder(func(_ context.Context, _ string, _ config.EinoConfig, structured StructuredOutputConfig) (model.BaseChatModel, error) {
			structuredCalls++
			captured = structured
			return chatModel, nil
		}),
	)

	_, err := NewProfileExtractorModel(factory, config.EinoConfig{Provider: "deepseek", Model: "profile-fast"}).Extract(context.Background(), profileextractor.ExtractRequest{
		Event: profileextractor.UpdateEvent{
			EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}, CreatedAt: time.Now(),
		},
		Messages: []*aimessages.AiMessages{{MsgId: "msg-1", UserId: 42, ConversationId: "conv-1", Role: "user", Content: "以后推荐轻薄手机"}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if plainCalls != 0 || structuredCalls != 1 {
		t.Fatalf("plainCalls=%d structuredCalls=%d", plainCalls, structuredCalls)
	}
	if captured.Name != "ai_user_profile_update" {
		t.Fatalf("structured output config = %+v", captured)
	}
	if captured.Description == "" {
		t.Fatalf("structured output config missing description: %+v", captured)
	}
}

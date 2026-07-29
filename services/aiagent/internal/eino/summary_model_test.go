package eino

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestSummarySummarizerUsesLLMWithPreviousSummaryAndMessages(t *testing.T) {
	response := schema.AssistantMessage(`{"summary":"压缩后的摘要","key_facts":{"sku":"p100"},"open_tasks":["等待确认"]}`, nil)
	response.ResponseMeta = &schema.ResponseMeta{
		Usage: &schema.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 42,
			TotalTokens:      142,
		},
	}
	chatModel := &capturingChatModel{response: response}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))
	summarizer := NewSummarySummarizer(factory, config.EinoConfig{Provider: "deepseek", Model: "summary-fast"})

	result, err := summarizer.Summarize(context.Background(), contextmanager.SummarizeRequest{
		UserID:         42,
		ConversationID: "conv-1",
		Previous: &domain.ConversationSummary{
			Summary:   "用户之前关注手机。",
			KeyFacts:  map[string]any{"budget": "3000"},
			OpenTasks: []string{"等待选择品牌"},
		},
		Messages: []*aimessages.AiMessages{
			{MsgId: "m001", Role: domain.ContextRoleUser, Content: "我想要轻薄一点", CreatedAt: time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)},
			{MsgId: "m002", Role: domain.ContextRoleAssistant, Content: "可以看看 p100", CreatedAt: time.Date(2026, 7, 24, 10, 1, 0, 0, time.UTC)},
		},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if result.RawOutput != `{"summary":"压缩后的摘要","key_facts":{"sku":"p100"},"open_tasks":["等待确认"]}` {
		t.Fatalf("raw = %q", result.RawOutput)
	}
	if result.PromptTokens != 100 || result.CompletionTokens != 42 || result.TotalTokens != 142 {
		t.Fatalf("usage = %+v, want 100/42/142", result)
	}
	if len(chatModel.messages) != 2 {
		t.Fatalf("model messages len = %d, want 2", len(chatModel.messages))
	}
	if chatModel.messages[0].Role != schema.System || !strings.Contains(chatModel.messages[0].Content, `"summary"`) {
		t.Fatalf("system prompt = %+v", chatModel.messages[0])
	}
	userPrompt := chatModel.messages[1].Content
	for _, want := range []string{"conv-1", "用户之前关注手机。", "等待选择品牌", "m001", "我想要轻薄一点", "m002", "可以看看 p100"} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("user prompt missing %q: %s", want, userPrompt)
		}
	}
}

func TestSummarySummarizerRejectsEmptyModelResponse(t *testing.T) {
	chatModel := &capturingChatModel{response: schema.AssistantMessage("   ", nil)}
	factory := NewModelFactory(WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		return chatModel, nil
	}))
	summarizer := NewSummarySummarizer(factory, config.EinoConfig{Provider: "deepseek", Model: "summary-fast"})

	_, err := summarizer.Summarize(context.Background(), contextmanager.SummarizeRequest{
		UserID:         42,
		ConversationID: "conv-1",
		Messages:       []*aimessages.AiMessages{{MsgId: "m001", Role: domain.ContextRoleUser, Content: "你好"}},
	})
	if !errors.Is(err, contextmanager.ErrInvalidSummaryOutput) {
		t.Fatalf("Summarize() error = %v, want ErrInvalidSummaryOutput", err)
	}
}

package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
)

func TestMemoryMiddlewareInjectsHistoryAndRuntimeContextOnce(t *testing.T) {
	provider := &fakeMemoryProvider{retrieve: &aimemory.RetrieveResult{
		HistoryMessages: []domain.ContextMessage{
			{Role: domain.ContextRoleUser, Content: "历史用户"},
			{Role: domain.ContextRoleAssistant, Content: "历史助手"},
		},
		ContextMessages: []domain.ContextMessage{
			{Role: domain.ContextRoleUser, Content: "<user_profile>\n{\"preferences\":{\"budget\":\"3000\"}}\n</user_profile>"},
		},
	}}
	middleware := NewMemoryMiddleware(provider)
	ctx := context.Background()
	ctx = aimemory.ContextWithConversationMetadata(ctx, aimemory.ConversationMetadata{
		UserID:           42,
		ConversationID:   "conv-1",
		CurrentMessageID: "msg-user",
		ClientMessageID:  "client-1",
		RunID:            "run-1",
	})
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("stable system"),
		schema.UserMessage("当前输入"),
	}}

	nextCtx, nextState, err := middleware.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState() error = %v", err)
	}
	if provider.retrieveCalls != 1 {
		t.Fatalf("retrieve calls = %d, want 1", provider.retrieveCalls)
	}
	if len(nextState.Messages) != 4 {
		t.Fatalf("messages len = %d, want system + history2 + current", len(nextState.Messages))
	}
	if nextState.Messages[0].Role != schema.System || nextState.Messages[0].Content != "stable system" {
		t.Fatalf("system message = %+v", nextState.Messages[0])
	}
	if nextState.Messages[1].Content != "历史用户" || nextState.Messages[2].Content != "历史助手" {
		t.Fatalf("history messages = %+v", nextState.Messages)
	}
	current := nextState.Messages[3]
	if current.Role != schema.User || !strings.Contains(current.Content, "当前输入") || !strings.Contains(current.Content, "<user_profile>") {
		t.Fatalf("current user message missing runtime context: %+v", current)
	}

	_, injectedAgain, err := middleware.BeforeModelRewriteState(nextCtx, nextState, nil)
	if err != nil {
		t.Fatalf("second BeforeModelRewriteState() error = %v", err)
	}
	if provider.retrieveCalls != 1 {
		t.Fatalf("retrieve calls after second invoke = %d, want still 1", provider.retrieveCalls)
	}
	if strings.Count(injectedAgain.Messages[3].Content, "<user_profile>") != 1 {
		t.Fatalf("runtime context duplicated: %q", injectedAgain.Messages[3].Content)
	}
}

func TestMemoryMiddlewareMemorizesOriginalUserAndFinalAssistantOnly(t *testing.T) {
	provider := &fakeMemoryProvider{retrieve: &aimemory.RetrieveResult{
		ContextMessages: []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: "<conversation_context>\n摘要\n</conversation_context>"}},
	}}
	middleware := NewMemoryMiddleware(provider)
	ctx := context.Background()
	ctx = aimemory.ContextWithConversationMetadata(ctx, aimemory.ConversationMetadata{
		UserID:           42,
		ConversationID:   "conv-1",
		CurrentMessageID: "msg-user",
		ClientMessageID:  "client-1",
		RunID:            "run-1",
	})
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("stable system"),
		schema.UserMessage("原始输入"),
	}}
	ctx, state, err := middleware.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState() error = %v", err)
	}
	state.Messages = append(state.Messages, schema.AssistantMessage("最终回答", nil))

	if _, _, err := middleware.AfterModelRewriteState(ctx, state, nil); err != nil {
		t.Fatalf("AfterModelRewriteState() error = %v", err)
	}
	if provider.memorizeCalls != 1 {
		t.Fatalf("memorize calls = %d, want 1", provider.memorizeCalls)
	}
	if len(provider.memorized.Messages) != 2 {
		t.Fatalf("memorized messages = %+v", provider.memorized.Messages)
	}
	if provider.memorized.Messages[0].Content != "原始输入" || strings.Contains(provider.memorized.Messages[0].Content, "<conversation_context>") {
		t.Fatalf("memorized user = %+v", provider.memorized.Messages[0])
	}
	if provider.memorized.Messages[1].Content != "最终回答" {
		t.Fatalf("memorized assistant = %+v", provider.memorized.Messages[1])
	}

	provider.memorizeCalls = 0
	toolCallState := &adk.ChatModelAgentState{Messages: append(state.Messages[:len(state.Messages)-1], &schema.Message{
		Role:    schema.Assistant,
		Content: "我要调用工具",
		ToolCalls: []schema.ToolCall{{ID: "call-1", Function: schema.FunctionCall{
			Name:      domain.ToolProductSearch,
			Arguments: `{"keyword":"手机"}`,
		}}},
	})}
	if _, _, err := middleware.AfterModelRewriteState(ctx, toolCallState, nil); err != nil {
		t.Fatalf("AfterModelRewriteState(tool call) error = %v", err)
	}
	if provider.memorizeCalls != 0 {
		t.Fatalf("tool-call assistant should not be memorized, calls=%d", provider.memorizeCalls)
	}
}

type fakeMemoryProvider struct {
	retrieve      *aimemory.RetrieveResult
	retrieveCalls int
	memorized     *aimemory.MemorizeRequest
	memorizeCalls int
}

func (f *fakeMemoryProvider) Retrieve(ctx context.Context, req *aimemory.RetrieveRequest) (*aimemory.RetrieveResult, error) {
	f.retrieveCalls++
	return f.retrieve, nil
}

func (f *fakeMemoryProvider) Memorize(ctx context.Context, req *aimemory.MemorizeRequest) error {
	f.memorizeCalls++
	f.memorized = req
	return nil
}

func (f *fakeMemoryProvider) Close() error {
	return nil
}

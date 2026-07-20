package logic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestChatRunsAssistantAndPersistsResponse(t *testing.T) {
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentChat}},
		AgentRunner: fakeAgentRunner{events: []domain.AgentEvent{{
			Type: domain.EventAssistantMessage, ConversationID: "conv-1", Content: "你好", Done: true,
		}}},
		MessagesModel: messages,
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "你好", Source: "web"})
	if err != nil || resp.StatusCode != 0 || len(resp.Events) != 1 {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	if resp.Events[0].MessageId == "" || resp.Events[0].MessageId == "user-1" {
		t.Fatalf("assistant message id must be generated independently: %+v", resp.Events[0])
	}
	if messages.batchCalls != 1 || messages.insertCalls != 0 || len(messages.inserted) != 1 || messages.inserted[0].Role != conversation.RoleAssistant {
		t.Fatalf("batchCalls=%d insertCalls=%d messages=%+v", messages.batchCalls, messages.insertCalls, messages.inserted)
	}
}

func TestChatDispatchesHighRiskToolToConfirmation(t *testing.T) {
	highRisk := &fakeChatToolExecutor{event: domain.AgentEvent{Type: domain.EventConfirmationRequired, Tool: domain.ToolOrderCancel, ConfirmationID: "confirm-1", Action: domain.ToolOrderCancel, Done: true}}
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		IntentPlanner: fakeIntentPlanner{result: planner.PlanResult{
			Intent: planner.IntentAction, ToolName: domain.ToolOrderCancel, Arguments: map[string]any{"order_id": "order-1", "user_id": 999}, RequireConfirmation: true,
		}},
		HighRiskChatTools: highRisk,
		MessagesModel:     messages,
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "取消订单 order-1"})
	if err != nil || len(resp.Events) != 1 || resp.Events[0].Type != domain.EventConfirmationRequired {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	if highRisk.req.UserID != 42 {
		t.Fatalf("trusted user id=%d", highRisk.req.UserID)
	}
	if _, exists := highRisk.req.Arguments["user_id"]; exists {
		t.Fatalf("untrusted user_id was retained: %+v", highRisk.req.Arguments)
	}
	if len(messages.inserted) != 1 || !strings.Contains(messages.inserted[0].Metadata.String, `"tool_name":"order.cancel"`) {
		t.Fatalf("tool metadata=%+v", messages.inserted)
	}
}

func TestChatReturnsAssistantPromptWithoutExecutingTool(t *testing.T) {
	query := &fakeChatToolExecutor{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentQuery, AssistantMessage: "请提供订单号", MissingParams: []string{"order_id"}}},
		QueryChatTools:      query,
		MessagesModel:       &fakeChatMessagesModel{},
	}
	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "查询订单"})
	if err != nil || len(resp.Events) != 1 || resp.Events[0].Content != "请提供订单号" {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	if query.calls != 0 {
		t.Fatalf("query tool calls=%d", query.calls)
	}
}

func TestChatRegistryForcesHighRiskConfirmation(t *testing.T) {
	highRisk := &fakeChatToolExecutor{event: domain.AgentEvent{Type: domain.EventConfirmationRequired, Tool: domain.ToolOrderCancel, Done: true}}
	registry := tools.NewRegistry(config.ToolTimeoutConfig{QuerySeconds: 3, WriteSeconds: 5})
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentAction, ToolName: domain.ToolOrderCancel, Arguments: map[string]any{"order_id": "order-1"}}},
		ToolRegistry:        registry,
		HighRiskChatTools:   highRisk,
		WriteChatTools:      &fakeChatToolExecutor{},
		MessagesModel:       &fakeChatMessagesModel{},
	}
	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "取消订单 order-1"})
	if err != nil || len(resp.Events) == 0 || resp.Events[0].Type != domain.EventConfirmationRequired || highRisk.calls != 1 {
		t.Fatalf("Chat() resp=%+v err=%v confirmation calls=%d", resp, err, highRisk.calls)
	}
}

func TestChatPreservesSuccessfulWriteWhenMessagePersistenceFails(t *testing.T) {
	write := &fakeChatToolExecutor{event: domain.AgentEvent{Type: domain.EventToolResult, Tool: domain.ToolCartAdd, Status: "success", Content: "已加入购物车", BusinessExecuted: true, Done: true}}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentAction, ToolName: domain.ToolCartAdd}},
		WriteChatTools:      write,
		MessagesModel:       &fakeChatMessagesModel{err: errors.New("insert failed")},
	}
	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "加入购物车"})
	if err != nil || len(resp.Events) != 3 || resp.Events[0].Type != domain.EventToolResult || resp.Events[0].Status != "success" || resp.Events[len(resp.Events)-1].Type != domain.EventError {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	messages := ctx.MessagesModel.(*fakeChatMessagesModel)
	if messages.batchCalls != 1 || messages.insertCalls != 0 || len(messages.inserted) != 2 {
		t.Fatalf("batchCalls=%d insertCalls=%d messages=%+v", messages.batchCalls, messages.insertCalls, messages.inserted)
	}
	if !strings.Contains(resp.Events[len(resp.Events)-1].DataJson, `"business_executed":true`) {
		t.Fatalf("persistence error=%+v", resp.Events[len(resp.Events)-1])
	}
}

type fakeConversationManager struct {
	prepared *conversation.PreparedConversation
	err      error
}

func (f fakeConversationManager) Prepare(context.Context, conversation.PrepareRequest) (*conversation.PreparedConversation, error) {
	return f.prepared, f.err
}

type fakeIntentPlanner struct {
	result planner.PlanResult
	err    error
}

func (f fakeIntentPlanner) Plan(context.Context, planner.PlanRequest) (planner.PlanResult, error) {
	return f.result, f.err
}

type fakeAgentRunner struct {
	events []domain.AgentEvent
	err    error
}

func (f fakeAgentRunner) Run(context.Context, eino.RunRequest) ([]domain.AgentEvent, error) {
	return f.events, f.err
}
func (f fakeAgentRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	return nil, f.err
}

type fakeChatToolExecutor struct {
	event domain.AgentEvent
	req   tools.ExecuteRequest
	calls int
}

func (f *fakeChatToolExecutor) Execute(_ context.Context, req tools.ExecuteRequest) domain.AgentEvent {
	f.calls++
	f.req = req
	return f.event
}

func (f *fakeChatToolExecutor) RequestConfirmation(_ context.Context, req tools.ExecuteRequest) domain.AgentEvent {
	return f.Execute(context.Background(), req)
}

type fakeChatMessagesModel struct {
	inserted    []*aimessages.AiMessages
	err         error
	batchCalls  int
	insertCalls int
}

func (f *fakeChatMessagesModel) Insert(_ context.Context, data *aimessages.AiMessages) (sql.Result, error) {
	f.insertCalls++
	f.inserted = append(f.inserted, data)
	return nil, f.err
}

func (f *fakeChatMessagesModel) InsertBatch(_ context.Context, data []*aimessages.AiMessages) error {
	f.batchCalls++
	f.inserted = append(f.inserted, data...)
	return f.err
}

func (f *fakeChatMessagesModel) FindOne(context.Context, string) (*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) Update(context.Context, *aimessages.AiMessages) error { return nil }
func (f *fakeChatMessagesModel) Delete(context.Context, string) error                 { return nil }
func (f *fakeChatMessagesModel) FindRecentByConversationID(context.Context, string, int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}

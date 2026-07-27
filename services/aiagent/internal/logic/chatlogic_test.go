package logic

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

func TestChatRunsAssistantAndPersistsResponse(t *testing.T) {
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		ContextManager:      successfulContextManager(),
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

func TestChatPublishesProfileUpdateAfterMessagesPersist(t *testing.T) {
	publisher := &fakeProfileUpdatePublisher{err: errors.New("kafka unavailable")}
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		ContextManager:      successfulContextManager(),
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentChat}},
		AgentRunner: fakeAgentRunner{events: []domain.AgentEvent{{
			Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "assistant-1", Content: "你好", Done: true,
		}}},
		MessagesModel:          messages,
		ProfileUpdatePublisher: publisher,
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "以后推荐轻薄手机", Source: "web"})
	if err != nil || resp.StatusCode != 0 || len(resp.Events) != 1 {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	if publisher.calls != 1 {
		t.Fatalf("profile update publisher calls=%d, want 1", publisher.calls)
	}
	if publisher.event.UserID != 42 || publisher.event.ConversationID != "conv-1" || publisher.event.EventID == "" || publisher.event.CreatedAt.IsZero() {
		t.Fatalf("profile update event = %+v", publisher.event)
	}
	wantIDs := map[string]bool{"user-1": true, "assistant-1": true}
	if len(publisher.event.MessageIDs) != len(wantIDs) {
		t.Fatalf("message ids = %+v", publisher.event.MessageIDs)
	}
	for _, id := range publisher.event.MessageIDs {
		if !wantIDs[id] {
			t.Fatalf("unexpected message id %q in event %+v", id, publisher.event)
		}
	}
}

func TestChatDispatchesHighRiskToolToConfirmation(t *testing.T) {
	highRisk := &fakeChatToolExecutor{event: domain.AgentEvent{Type: domain.EventConfirmationRequired, Tool: domain.ToolOrderCancel, ConfirmationID: "confirm-1", Action: domain.ToolOrderCancel, Done: true}}
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		ContextManager:      successfulContextManager(),
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
		ContextManager:      successfulContextManager(),
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
		ContextManager:      successfulContextManager(),
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
		ContextManager:      successfulContextManager(),
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

func TestChatPersistsSuccessfulToolResultEnvelope(t *testing.T) {
	write := &fakeChatToolExecutor{event: domain.AgentEvent{
		Type: domain.EventToolResult, Tool: domain.ToolCartAdd, Status: "success",
		Content: "已加入购物车", DataJSON: `{"cart_item_id":2,"product_id":9,"quantity":1}`, Done: true,
	}}
	messages := &fakeChatMessagesModel{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		ContextManager:      successfulContextManager(),
		IntentPlanner:       fakeIntentPlanner{result: planner.PlanResult{Intent: planner.IntentAction, ToolName: domain.ToolCartAdd}},
		WriteChatTools:      write,
		MessagesModel:       messages,
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "加入购物车"})
	if err != nil || resp.StatusCode != 0 || len(messages.inserted) != 2 {
		t.Fatalf("Chat() resp=%+v err=%v messages=%+v", resp, err, messages.inserted)
	}
	toolMessage := messages.inserted[0]
	if toolMessage.Role != conversation.RoleTool || toolMessage.Id == "" {
		t.Fatalf("tool message = %+v", toolMessage)
	}
	var meta struct {
		ToolCallID string `json:"tool_call_id"`
		ToolName   string `json:"tool_name"`
		Status     string `json:"status"`
		DataJSON   string `json:"data_json"`
		ToolResult struct {
			ToolCallID string          `json:"tool_call_id"`
			ToolName   string          `json:"tool_name"`
			Status     string          `json:"status"`
			Data       json.RawMessage `json:"data"`
			Summary    string          `json:"summary"`
		} `json:"tool_result"`
	}
	if err := json.Unmarshal([]byte(toolMessage.Metadata.String), &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if meta.ToolCallID != toolMessage.Id || meta.ToolResult.ToolCallID != toolMessage.Id {
		t.Fatalf("tool_call_id meta=%q envelope=%q message=%q", meta.ToolCallID, meta.ToolResult.ToolCallID, toolMessage.Id)
	}
	var rawMeta map[string]any
	if err := json.Unmarshal([]byte(toolMessage.Metadata.String), &rawMeta); err != nil {
		t.Fatalf("metadata map json: %v", err)
	}
	toolResult, _ := rawMeta["tool_result"].(map[string]any)
	if meta.DataJSON == "" || meta.ToolResult.Summary != "已加入购物车" || hasUnexpectedToolResultVersionKey(toolResult) {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestChatUsesContextManagerForPlannerAndRunnerWithoutHistoryFallback(t *testing.T) {
	contextBuilder := &fakeBuildContextManager{
		intent: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "intent-system"},
			{Role: domain.ContextRoleUser, Content: "你好"},
		},
		agent: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "agent-system"},
			{Role: domain.ContextRoleAssistant, Content: "context-manager-only"},
			{Role: domain.ContextRoleUser, Content: "你好"},
		},
	}
	intentPlanner := &capturingIntentPlanner{result: planner.PlanResult{
		Intent: planner.IntentChat, AssistantMessage: "fast intent model reply must not bypass AgentRunner",
	}}
	agentRunner := &capturingAgentRunner{events: []domain.AgentEvent{{
		Type: domain.EventAssistantMessage, ConversationID: "conv-1", Content: "你好", Done: true,
	}}}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{
			ConversationID: "conv-1", UserMessageID: "user-1",
			History: []*aimessages.AiMessages{{Id: "legacy", Role: conversation.RoleAssistant, Content: "legacy-history-must-not-be-used"}},
		}},
		ContextManager: contextBuilder,
		IntentPlanner:  intentPlanner,
		AgentRunner:    agentRunner,
		MessagesModel:  &fakeChatMessagesModel{},
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{
		UserId: 42, ConversationId: "conv-1", MessageId: "user-1", Content: "你好",
	})
	if err != nil || resp.StatusCode != 0 {
		t.Fatalf("Chat() resp=%+v err=%v", resp, err)
	}
	if len(contextBuilder.requests) != 2 ||
		contextBuilder.requests[0].Mode != domain.IntentContextMode ||
		contextBuilder.requests[1].Mode != domain.AgentContextMode {
		t.Fatalf("context requests = %+v", contextBuilder.requests)
	}
	for _, request := range contextBuilder.requests {
		if request.UserID != 42 || request.ConversationID != "conv-1" || request.CurrentMessageID != "user-1" || request.CurrentInput != "你好" {
			t.Fatalf("context request = %+v", request)
		}
	}
	if joinContextContentsForLogic(intentPlanner.req.Messages) != "intent-system\n你好\n" {
		t.Fatalf("planner messages = %+v", intentPlanner.req.Messages)
	}
	runnerContext := joinContextContentsForLogic(agentRunner.req.Messages)
	if !strings.Contains(runnerContext, "context-manager-only") || strings.Contains(runnerContext, "legacy-history-must-not-be-used") {
		t.Fatalf("runner messages = %+v", agentRunner.req.Messages)
	}
}

func TestChatReturnsErrorWhenContextBuildFailsWithoutCallingPlanner(t *testing.T) {
	contextErr := errors.New("context build failed")
	intentPlanner := &capturingIntentPlanner{}
	ctx := &svc.ServiceContext{
		ConversationManager: fakeConversationManager{prepared: &conversation.PreparedConversation{ConversationID: "conv-1", UserMessageID: "user-1"}},
		ContextManager:      &fakeBuildContextManager{err: contextErr},
		IntentPlanner:       intentPlanner,
		MessagesModel:       &fakeChatMessagesModel{},
	}

	resp, err := NewChatLogic(context.Background(), ctx).Chat(&aiagent.ChatRequest{UserId: 42, Content: "你好"})
	if err != nil || resp.StatusCode == 0 || intentPlanner.calls != 0 {
		t.Fatalf("Chat() resp=%+v err=%v planner_calls=%d", resp, err, intentPlanner.calls)
	}
	if !strings.Contains(resp.StatusMsg, contextErr.Error()) {
		t.Fatalf("StatusMsg = %q", resp.StatusMsg)
	}
}

func hasUnexpectedToolResultVersionKey(value map[string]any) bool {
	for key := range value {
		if key == "schema"+"_"+"version" {
			return true
		}
	}
	return false
}

func TestChatDoesNotPersistFullEnvelopeForConfirmationOrFailedTool(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event domain.AgentEvent
	}{
		{name: "confirmation", event: domain.AgentEvent{Type: domain.EventConfirmationRequired, Tool: domain.ToolOrderCancel, Status: "pending", ConfirmationID: "confirm-1", Done: true}},
		{name: "failed", event: domain.AgentEvent{Type: domain.EventToolResult, Tool: domain.ToolCartList, Status: "failed", DataJSON: `{"error":"boom"}`, Done: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message, err := agentEventToMessage(42, tc.event)
			if err != nil {
				t.Fatalf("agentEventToMessage() error = %v", err)
			}
			if strings.Contains(message.Metadata.String, `"tool_result"`) {
				t.Fatalf("metadata should not contain full tool_result: %s", message.Metadata.String)
			}
		})
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

type capturingIntentPlanner struct {
	result planner.PlanResult
	err    error
	req    planner.PlanRequest
	calls  int
}

func (f *capturingIntentPlanner) Plan(_ context.Context, req planner.PlanRequest) (planner.PlanResult, error) {
	f.calls++
	f.req = req
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

type capturingAgentRunner struct {
	events []domain.AgentEvent
	err    error
	req    eino.RunRequest
}

func (f *capturingAgentRunner) Run(_ context.Context, req eino.RunRequest) ([]domain.AgentEvent, error) {
	f.req = req
	return f.events, f.err
}

func (f *capturingAgentRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	return nil, f.err
}

type fakeBuildContextManager struct {
	intent   []domain.ContextMessage
	agent    []domain.ContextMessage
	err      error
	requests []domain.BuildContextRequest
}

func (f *fakeBuildContextManager) Build(_ context.Context, req domain.BuildContextRequest) (*domain.BuildContextResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	messages := f.intent
	if req.Mode == domain.AgentContextMode {
		messages = f.agent
	}
	return &domain.BuildContextResult{Messages: messages}, nil
}

func successfulContextManager() *fakeBuildContextManager {
	return &fakeBuildContextManager{
		intent: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "intent-system"},
			{Role: domain.ContextRoleUser, Content: "current"},
		},
		agent: []domain.ContextMessage{
			{Role: domain.ContextRoleSystem, Content: "agent-system"},
			{Role: domain.ContextRoleUser, Content: "current"},
		},
	}
}

func joinContextContentsForLogic(messages []domain.ContextMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(message.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
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

type fakeProfileUpdatePublisher struct {
	event profileextractor.UpdateEvent
	err   error
	calls int
}

func (f *fakeProfileUpdatePublisher) PublishProfileUpdate(_ context.Context, event profileextractor.UpdateEvent) error {
	f.calls++
	f.event = event
	return f.err
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
func (f *fakeChatMessagesModel) FindRecentContextMessages(context.Context, uint64, string, int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) CountUnsummarizedContextMessages(context.Context, uint64, string, string, string) (int64, error) {
	return 0, nil
}
func (f *fakeChatMessagesModel) FindUnsummarizedContextMessages(context.Context, uint64, string, string, string, int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindRecentUnsummarizedContextMessages(context.Context, uint64, string, string, string, int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindRecentToolMessages(context.Context, uint64, string, int) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindToolMessageByID(context.Context, uint64, string, string) (*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindMessagesByIDs(context.Context, uint64, string, []string) ([]*aimessages.AiMessages, error) {
	return nil, nil
}

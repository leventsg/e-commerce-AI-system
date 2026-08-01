package logic

import (
	"context"
	"database/sql"
	"testing"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
)

func TestChatLogicPersistsADKToolAndAssistantEventsViaCallback(t *testing.T) {
	messages := &fakeChatMessagesModel{}
	logic := NewChatLogic(context.Background(), &svc.ServiceContext{
		ConversationManager: &fakeConversationManager{prepared: &conversation.PreparedConversation{
			ConversationID:  "conv-1",
			UserMessageID:   "msg-user",
			ClientMessageID: "client-1",
		}},
		ContextManager: &fakeContextManager{},
		IntentPlanner:  fakeIntentPlanner{plan: planner.PlanResult{Intent: planner.IntentChat}},
		AgentRunner: fakeRunner{events: []domain.AgentEvent{
			{Type: domain.EventToolResult, ConversationID: "conv-1", MessageID: "msg-tool", ToolCallID: "call-1", Tool: "cart.list", Status: "success", Content: `{"items":[]}`, DataJSON: `{"items":[]}`, Done: true},
			{Type: domain.EventAssistantMessage, ConversationID: "conv-1", MessageID: "msg-assistant", Content: "购物车为空", Done: true},
		}},
		MessagesModel: messages,
	})

	resp, err := logic.Chat(&aiagent.ChatRequest{
		UserId:          42,
		ConversationId:  "conv-1",
		Content:         "查购物车",
		ClientMessageId: "client-1",
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.StatusCode != 0 || len(resp.Events) != 2 {
		t.Fatalf("response = %+v", resp)
	}
	if len(messages.batches) != 2 {
		t.Fatalf("InsertBatch calls = %d, want callback-only 2 calls", len(messages.batches))
	}
	if got := messages.batches[0][0]; got.Role != conversation.RoleTool || got.MsgId != "msg-tool" || !got.ClientMessageId.Valid || got.ClientMessageId.String != "client-1" {
		t.Fatalf("tool message = %+v", got)
	}
	if got := messages.batches[1][0]; got.Role != conversation.RoleAssistant || got.MsgId != "msg-assistant" || !got.ClientMessageId.Valid || got.ClientMessageId.String != "client-1" {
		t.Fatalf("assistant message = %+v", got)
	}
	if !messages.batches[0][0].Metadata.Valid || messages.batches[0][0].Metadata.String == "" {
		t.Fatalf("tool metadata missing: %+v", messages.batches[0][0])
	}
}

type fakeConversationManager struct {
	prepared *conversation.PreparedConversation
}

func (f *fakeConversationManager) Prepare(context.Context, conversation.PrepareRequest) (*conversation.PreparedConversation, error) {
	return f.prepared, nil
}

type fakeContextManager struct{}

func (f *fakeContextManager) Build(_ context.Context, req domain.BuildContextRequest) (*domain.BuildContextResult, error) {
	return &domain.BuildContextResult{Messages: []domain.ContextMessage{{Role: domain.ContextRoleUser, Content: req.CurrentInput}}}, nil
}

type fakeIntentPlanner struct {
	plan planner.PlanResult
}

func (f fakeIntentPlanner) Plan(context.Context, planner.PlanRequest) (planner.PlanResult, error) {
	return f.plan, nil
}

type fakeRunner struct {
	events []domain.AgentEvent
}

func (f fakeRunner) Run(ctx context.Context, req eino.RunRequest) ([]domain.AgentEvent, error) {
	for _, event := range f.events {
		if req.OnEvent != nil {
			if err := req.OnEvent(ctx, event); err != nil {
				return nil, err
			}
		}
	}
	return f.events, nil
}

func (f fakeRunner) Stream(context.Context, eino.RunRequest) (<-chan domain.AgentEvent, error) {
	return nil, nil
}

type fakeChatMessagesModel struct {
	batches [][]*aimessages.AiMessages
}

func (f *fakeChatMessagesModel) InsertBatch(_ context.Context, messages []*aimessages.AiMessages) error {
	f.batches = append(f.batches, messages)
	return nil
}

func (f *fakeChatMessagesModel) Insert(context.Context, *aimessages.AiMessages) (sql.Result, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindOne(context.Context, uint64) (*aimessages.AiMessages, error) {
	return nil, aimessages.ErrNotFound
}
func (f *fakeChatMessagesModel) FindOneByMsgId(context.Context, string) (*aimessages.AiMessages, error) {
	return nil, aimessages.ErrNotFound
}
func (f *fakeChatMessagesModel) FindOneByUserIdDedupeClientMessageId(context.Context, uint64, sql.NullString) (*aimessages.AiMessages, error) {
	return nil, aimessages.ErrNotFound
}
func (f *fakeChatMessagesModel) Update(context.Context, *aimessages.AiMessages) error { return nil }
func (f *fakeChatMessagesModel) Delete(context.Context, uint64) error                 { return nil }
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
	return nil, aimessages.ErrNotFound
}
func (f *fakeChatMessagesModel) FindMessagesByIDs(context.Context, uint64, string, []string) ([]*aimessages.AiMessages, error) {
	return nil, nil
}
func (f *fakeChatMessagesModel) FindUserMessageByClientMessageID(context.Context, uint64, string) (*aimessages.AiMessages, error) {
	return nil, aimessages.ErrNotFound
}
func (f *fakeChatMessagesModel) FindAssistantMessagesByClientMessageID(context.Context, uint64, string, string) ([]*aimessages.AiMessages, error) {
	return nil, nil
}

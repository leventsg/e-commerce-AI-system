package contextmanager

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestToolResultStoreRestoresLatestSuccessfulEnvelope(t *testing.T) {
	model := fakeToolMessagesModel{recent: []*aimessages.AiMessages{
		toolMessage(t, "call-new", 42, "conv-1", domain.ToolCartAdd, "success", `{"cart_item_id":2,"product_id":9,"quantity":1}`, "已加入购物车", baseTime().Add(time.Minute)),
		toolMessage(t, "call-old", 42, "conv-1", domain.ToolCartList, "success", `{"items":[{"cart_item_id":1}]}`, "旧结果", baseTime()),
	}}
	store := NewToolResultStore(&model)

	result, err := store.FindLatestResult(context.Background(), 42, "conv-1")
	if err != nil {
		t.Fatalf("FindLatestResult() error = %v", err)
	}
	if result.ToolCallID != "call-new" || result.ToolName != domain.ToolCartAdd || result.Status != "success" {
		t.Fatalf("latest result = %+v", result)
	}
	var data map[string]any
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["cart_item_id"].(float64) != 2 {
		t.Fatalf("data = %+v", data)
	}
}

func TestToolResultStoreSkipsFailuresInvalidJSONAndUnknownTool(t *testing.T) {
	model := fakeToolMessagesModel{recent: []*aimessages.AiMessages{
		toolMessage(t, "call-good", 42, "conv-1", domain.ToolCartList, "success", `{"items":[]}`, "有效", baseTime().Add(3*time.Minute)),
		rawToolMessage("call-unknown", 42, "conv-1", baseTime().Add(2*time.Minute), `{"tool_call_id":"call-unknown","tool_name":"unknown.tool","status":"success","tool_result":{"tool_call_id":"call-unknown","tool_name":"unknown.tool","status":"success","data":{},"summary":"unknown"}}`),
		rawToolMessage("call-invalid", 42, "conv-1", baseTime().Add(time.Minute), `{"tool_call_id":"call-invalid","tool_name":"cart.list","status":"success","tool_result":{`),
		toolMessage(t, "call-failed", 42, "conv-1", domain.ToolCartList, "failed", `{"error":"boom"}`, "失败", baseTime()),
	}}
	store := NewToolResultStore(&model)

	result, err := store.FindLatestResult(context.Background(), 42, "conv-1")
	if err != nil {
		t.Fatalf("FindLatestResult() error = %v", err)
	}
	if result.ToolCallID != "call-good" {
		t.Fatalf("latest valid result = %+v", result)
	}
}

func TestToolResultStoreBuildsRefsAndLegacyDataJSONOnlyAsRefs(t *testing.T) {
	legacyMetadata := `{"tool_call_id":"call-legacy","tool_name":"cart.list","status":"success","data_json":"{\"items\":[{\"cart_item_id\":7,\"product_id\":11,\"quantity\":2}]}"}`
	model := fakeToolMessagesModel{recent: []*aimessages.AiMessages{
		toolMessage(t, "call-new", 42, "conv-1", domain.ToolCartAdd, "success", `{"cart_item_id":8,"product_id":12,"quantity":1}`, "已加入购物车", baseTime().Add(time.Minute)),
		rawToolMessage("call-legacy", 42, "conv-1", baseTime(), legacyMetadata),
	}}
	store := NewToolResultStore(&model)

	refs, err := store.FindRecentRefs(context.Background(), 42, "conv-1", 10)
	if err != nil {
		t.Fatalf("FindRecentRefs() error = %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2: %+v", len(refs), refs)
	}
	assertRefHas(t, refs[0], "product_ids", "12")
	assertRefHas(t, refs[1], "cart_item_ids", "7")

	model.one = rawToolMessage("call-legacy", 42, "conv-1", baseTime(), legacyMetadata)
	if _, err := store.FindResultByCallID(context.Background(), 42, "conv-1", "call-legacy"); !errors.Is(err, ErrToolResultUnavailable) {
		t.Fatalf("legacy FindResultByCallID error = %v, want ErrToolResultUnavailable", err)
	}
}

func TestBuildToolResultMetadataDoesNotNormalizeInternalFields(t *testing.T) {
	metadata, err := BuildToolResultMetadata(" call-1 ", domain.ToolCartList, "success", " confirm-1 ", `{"items":[]}`, " summary ")
	if err != nil {
		t.Fatalf("BuildToolResultMetadata() error = %v", err)
	}
	var meta toolMessageMetadata
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if meta.ToolCallID != " call-1 " || meta.ConfirmationID != " confirm-1 " || meta.ToolResult.Summary != " summary " {
		t.Fatalf("metadata was normalized unexpectedly: %+v", meta)
	}
}

func TestToolResultStoreRefsSkipFailedAndUnknownTool(t *testing.T) {
	model := fakeToolMessagesModel{recent: []*aimessages.AiMessages{
		toolMessage(t, "call-good", 42, "conv-1", domain.ToolCartList, "success", `{"items":[{"cart_item_id":3}]}`, "有效", baseTime().Add(2*time.Minute)),
		rawToolMessage("call-unknown", 42, "conv-1", baseTime().Add(time.Minute), `{"tool_call_id":"call-unknown","tool_name":"unknown.tool","status":"success","data_json":"{\"items\":[{\"cart_item_id\":2}]}","tool_result":{"tool_call_id":"call-unknown","tool_name":"unknown.tool","status":"success","data":{"items":[{"cart_item_id":2}]},"summary":"unknown"}}`),
		toolMessage(t, "call-failed", 42, "conv-1", domain.ToolCartList, "failed", `{"items":[{"cart_item_id":1}]}`, "失败", baseTime()),
	}}
	store := NewToolResultStore(&model)

	refs, err := store.FindRecentRefs(context.Background(), 42, "conv-1", 10)
	if err != nil {
		t.Fatalf("FindRecentRefs() error = %v", err)
	}
	if len(refs) != 1 || refs[0].ToolCallID != "call-good" {
		t.Fatalf("refs = %+v, want only call-good", refs)
	}
}

func TestToolResultStoreFindByCallIDRequiresUserConversationAndToolRole(t *testing.T) {
	model := fakeToolMessagesModel{one: toolMessage(t, "call-1", 42, "conv-1", domain.ToolOrderGet, "success", `{"order_id":"order-1","status":"paid"}`, "订单已支付", baseTime())}
	store := NewToolResultStore(&model)

	if _, err := store.FindResultByCallID(context.Background(), 7, "conv-1", "call-1"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong user error = %v, want ErrToolResultNotFound", err)
	}
	if _, err := store.FindResultByCallID(context.Background(), 42, "conv-2", "call-1"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong conversation error = %v, want ErrToolResultNotFound", err)
	}
	if _, err := store.FindResultByCallID(context.Background(), 42, "conv-1", "call-2"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong call error = %v, want ErrToolResultNotFound", err)
	}

	result, err := store.FindResultByCallID(context.Background(), 42, "conv-1", "call-1")
	if err != nil {
		t.Fatalf("FindResultByCallID() error = %v", err)
	}
	if result.ToolCallID != "call-1" || result.ToolName != domain.ToolOrderGet {
		t.Fatalf("result = %+v", result)
	}
}

type fakeToolMessagesModel struct {
	recent []*aimessages.AiMessages
	one    *aimessages.AiMessages
}

func (f *fakeToolMessagesModel) FindRecentToolMessages(context.Context, uint64, string, int) ([]*aimessages.AiMessages, error) {
	return f.recent, nil
}

func (f *fakeToolMessagesModel) FindToolMessageByID(_ context.Context, userID uint64, conversationID, messageID string) (*aimessages.AiMessages, error) {
	if f.one == nil || f.one.UserId != userID || f.one.ConversationId != conversationID || f.one.MsgId != messageID {
		return nil, sql.ErrNoRows
	}
	return f.one, nil
}

func toolMessage(t *testing.T, id string, userID uint64, conversationID, toolName, status, dataJSON, summary string, createdAt time.Time) *aimessages.AiMessages {
	t.Helper()
	metadata, err := BuildToolResultMetadata(id, toolName, status, "", dataJSON, summary)
	if err != nil {
		t.Fatalf("BuildToolResultMetadata() error = %v", err)
	}
	return rawToolMessage(id, userID, conversationID, createdAt, metadata)
}

func rawToolMessage(id string, userID uint64, conversationID string, createdAt time.Time, metadata string) *aimessages.AiMessages {
	return &aimessages.AiMessages{
		MsgId:          id,
		ConversationId: conversationID,
		UserId:         userID,
		Role:           conversation.RoleTool,
		Content:        "summary",
		Metadata:       sql.NullString{String: metadata, Valid: metadata != ""},
		CreatedAt:      createdAt,
	}
}

func baseTime() time.Time {
	return time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
}

func assertRefHas(t *testing.T, ref domain.ToolCallRef, key, value string) {
	t.Helper()
	for _, got := range ref.EntityIDs[key] {
		if got == value {
			return
		}
	}
	t.Fatalf("ref %+v missing %s=%s", ref, key, value)
}

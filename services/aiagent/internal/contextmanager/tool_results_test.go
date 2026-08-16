package contextmanager

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestToolCallStoreReturnsLatestRawToolCall(t *testing.T) {
	model := fakeToolCallsModel{recent: []*aitoolcalls.AiToolCalls{
		toolCallRow(2, "call-new", 42, "conv-1", domain.ToolCartAdd, "success", `{"cart_item_id":2,"product_id":9,"quantity":1}`, baseTime().Add(time.Minute)),
		toolCallRow(1, "call-old", 42, "conv-1", domain.ToolCartList, "success", `{"items":[{"cart_item_id":1}]}`, baseTime()),
	}}
	store := NewToolCallStore(&model)

	result, err := store.FindLatestToolResult(context.Background(), 42, "conv-1")
	if err != nil {
		t.Fatalf("FindLatestToolResult() error = %v", err)
	}
	if result.ToolCallId != "call-new" || result.ToolName != domain.ToolCartAdd || result.Result == "" {
		t.Fatalf("latest result = %+v", result)
	}
}

func TestToolCallStoreReturnsRecentRawToolCalls(t *testing.T) {
	model := fakeToolCallsModel{recent: []*aitoolcalls.AiToolCalls{
		toolCallRow(2, "call-new", 42, "conv-1", domain.ToolCartAdd, "success", `{"cart_item_id":8,"product_id":12,"quantity":1}`, baseTime().Add(time.Minute)),
		toolCallRow(1, "call-old", 42, "conv-1", domain.ToolCartList, "success", `{"items":[{"cart_item_id":7,"product_id":11,"quantity":2}]}`, baseTime()),
	}}
	store := NewToolCallStore(&model)

	calls, err := store.FindRecentToolCall(context.Background(), 42, "conv-1", 10)
	if err != nil {
		t.Fatalf("FindRecentToolCall() error = %v", err)
	}
	if len(calls) != 2 || calls[0].ToolCallId != "call-new" || calls[1].ToolCallId != "call-old" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestBuildToolResultMetadataDoesNotEmbedWrappedResult(t *testing.T) {
	metadata, err := BuildToolResultMetadata(" call-1 ", domain.ToolCartList, "success", " confirm-1 ", `{"items":[]}`, " summary ")
	if err != nil {
		t.Fatalf("BuildToolResultMetadata() error = %v", err)
	}
	var meta toolMessageMetadata
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if meta.ToolCallID != " call-1 " || meta.ConfirmationID != " confirm-1 " || meta.DataJSON != `{"items":[]}` {
		t.Fatalf("metadata was normalized unexpectedly: %+v", meta)
	}
	if strings.Contains(metadata, "tool_result") || strings.Contains(metadata, "summary") {
		t.Fatalf("metadata should not embed wrapped tool result: %s", metadata)
	}
}

func TestToolCallStoreFindByCallIDReturnsRawToolCall(t *testing.T) {
	model := fakeToolCallsModel{one: toolCallRow(1, "call-1", 42, "conv-1", domain.ToolOrderGet, "success", `{"order_id":"order-1","status":"paid"}`, baseTime())}
	store := NewToolCallStore(&model)

	if _, err := store.FindToolCallByCallID(context.Background(), 7, "conv-1", "call-1"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong user error = %v, want ErrToolResultNotFound", err)
	}
	if _, err := store.FindToolCallByCallID(context.Background(), 42, "conv-2", "call-1"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong conversation error = %v, want ErrToolResultNotFound", err)
	}
	if _, err := store.FindToolCallByCallID(context.Background(), 42, "conv-1", "call-2"); !errors.Is(err, ErrToolResultNotFound) {
		t.Fatalf("wrong call error = %v, want ErrToolResultNotFound", err)
	}

	result, err := store.FindToolCallByCallID(context.Background(), 42, "conv-1", "call-1")
	if err != nil {
		t.Fatalf("FindToolCallByCallID() error = %v", err)
	}
	if result.ToolCallId != "call-1" || result.ToolName != domain.ToolOrderGet || result.Result == "" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeToolCallsModel struct {
	recent []*aitoolcalls.AiToolCalls
	one    *aitoolcalls.AiToolCalls
}

func (f *fakeToolCallsModel) FindRecentSuccessfulToolCalls(context.Context, uint64, string, int) ([]*aitoolcalls.AiToolCalls, error) {
	return f.recent, nil
}

func (f *fakeToolCallsModel) FindToolCallByCallID(_ context.Context, userID uint64, conversationID, toolCallID string) (*aitoolcalls.AiToolCalls, error) {
	if f.one == nil || f.one.UserId != userID || f.one.ConversationId != conversationID || f.one.ToolCallId != toolCallID {
		return nil, sql.ErrNoRows
	}
	return f.one, nil
}

func toolCallRow(id uint64, toolCallID string, userID uint64, conversationID, toolName, status, result string, createdAt time.Time) *aitoolcalls.AiToolCalls {
	return &aitoolcalls.AiToolCalls{
		Id:             id,
		ToolCallId:     toolCallID,
		ConversationId: conversationID,
		UserId:         userID,
		ToolName:       toolName,
		Status:         status,
		Result:         result,
		CreatedAt:      createdAt,
	}
}

func baseTime() time.Time {
	return time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
}

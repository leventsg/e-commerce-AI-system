package audit

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/leventsg/e-commerce-AI-system/services/audit/auditclient"
	"google.golang.org/grpc"
)

func TestRecorderPersistsToolCallAndAuditsWrite(t *testing.T) {
	model := &fakeToolCallModel{}
	auditRPC := &fakeAuditRPC{resp: &auditclient.CreateAuditLogRes{Ok: true}}
	recorder := NewRecorder(model, auditRPC)

	err := recorder.RecordToolCall(context.Background(), tools.ToolCallRecord{
		ConversationID: "conv-1",
		UserID:         42,
		ToolName:       domain.ToolCartAdd,
		Arguments: map[string]any{
			"product_id": 12,
			"quantity":   2,
			"user_id":    999,
			"nested": map[string]any{
				"token": "secret",
			},
		},
		Status:        "success",
		ResultSummary: "已加入购物车。",
		ResultData:    map[string]any{"cart_item_id": 8, "product_id": 12},
		Latency:       1500 * time.Millisecond,
		Metadata:      domain.Metadata{WriteOperation: true},
	})
	if err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}

	if model.row == nil || model.row.Id == "" {
		t.Fatalf("tool call row = %#v, want generated ID", model.row)
	}
	if model.row.ConversationId != "conv-1" || model.row.UserId != 42 || model.row.ToolName != domain.ToolCartAdd {
		t.Fatalf("tool call row identity = %#v", model.row)
	}
	if model.row.LatencyMs != 1500 || !model.row.ResultSummary.Valid {
		t.Fatalf("tool call row result = %#v", model.row)
	}
	if strings.Contains(model.row.Arguments, "user_id") || strings.Contains(model.row.Arguments, "token") {
		t.Fatalf("sensitive arguments persisted: %s", model.row.Arguments)
	}

	if auditRPC.req == nil {
		t.Fatal("write operation did not call audit RPC")
	}
	if auditRPC.req.UserId != 42 || auditRPC.req.ActionType != "create" || auditRPC.req.TargetTable != "cart" || auditRPC.req.TargetId != 8 {
		t.Fatalf("audit request target = %#v", auditRPC.req)
	}
	if auditRPC.req.ClientIp != "0.0.0.0" || auditRPC.req.ServiceName != "aiagent" {
		t.Fatalf("audit request source = %#v", auditRPC.req)
	}
	if strings.Contains(auditRPC.req.NewData, "user_id") || !strings.Contains(auditRPC.req.NewData, `"status":"success"`) {
		t.Fatalf("audit new_data = %s", auditRPC.req.NewData)
	}
}

func TestRecorderUsesExecutionIPAndCouponUserTarget(t *testing.T) {
	model := &fakeToolCallModel{}
	auditRPC := &fakeAuditRPC{resp: &auditclient.CreateAuditLogRes{Ok: true}}
	recorder := NewRecorder(model, auditRPC)

	err := recorder.RecordToolCall(context.Background(), tools.ToolCallRecord{
		ConversationID: "conv-2",
		UserID:         42,
		ToolName:       domain.ToolCouponClaim,
		Arguments:      map[string]any{"coupon_id": "coupon-1"},
		Status:         "failed",
		ErrorMessage:   "already claimed",
		ClientIP:       "203.0.113.8",
		Metadata:       domain.Metadata{WriteOperation: true},
	})
	if err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	if auditRPC.req.TargetTable != "user_coupons" || auditRPC.req.TargetId != 42 {
		t.Fatalf("coupon audit target = %#v", auditRPC.req)
	}
	if auditRPC.req.ClientIp != "203.0.113.8" || !strings.Contains(auditRPC.req.NewData, "coupon-1") || !strings.Contains(auditRPC.req.NewData, "already claimed") {
		t.Fatalf("coupon audit data = %#v", auditRPC.req)
	}
}

func TestRecorderDoesNotAuditReadOperation(t *testing.T) {
	model := &fakeToolCallModel{}
	auditRPC := &fakeAuditRPC{resp: &auditclient.CreateAuditLogRes{Ok: true}}
	recorder := NewRecorder(model, auditRPC)

	err := recorder.RecordToolCall(context.Background(), tools.ToolCallRecord{
		ConversationID: "conv-3",
		UserID:         42,
		ToolName:       domain.ToolProductDetail,
		Arguments:      map[string]any{"product_id": 12},
		Status:         "success",
		Metadata:       domain.Metadata{WriteOperation: false},
	})
	if err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	if model.row == nil {
		t.Fatal("read operation was not persisted to ai_tool_calls")
	}
	if auditRPC.req != nil {
		t.Fatalf("read operation unexpectedly called audit RPC: %#v", auditRPC.req)
	}
}

func TestRecorderAttemptsBothPersistencePaths(t *testing.T) {
	modelErr := errors.New("tool call insert failed")
	auditErr := errors.New("audit rpc failed")
	model := &fakeToolCallModel{err: modelErr}
	auditRPC := &fakeAuditRPC{err: auditErr}
	recorder := NewRecorder(model, auditRPC)

	err := recorder.RecordToolCall(context.Background(), tools.ToolCallRecord{
		UserID:    42,
		ToolName:  domain.ToolCartSub,
		Arguments: map[string]any{"cart_item_id": 8, "quantity": 1},
		Status:    "failed",
		Metadata:  domain.Metadata{WriteOperation: true},
	})
	if !errors.Is(err, modelErr) || !errors.Is(err, auditErr) {
		t.Fatalf("RecordToolCall error = %v, want both failures", err)
	}
	if model.calls != 1 || auditRPC.calls != 1 {
		t.Fatalf("record attempts model=%d audit=%d, want both 1", model.calls, auditRPC.calls)
	}
}

func TestRecorderDetachesPersistenceFromCanceledRequest(t *testing.T) {
	model := &fakeToolCallModel{honorContext: true}
	auditRPC := &fakeAuditRPC{resp: &auditclient.CreateAuditLogRes{Ok: true}, honorContext: true}
	recorder := NewRecorder(model, auditRPC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := recorder.RecordToolCall(ctx, tools.ToolCallRecord{
		UserID:    42,
		ToolName:  domain.ToolCartAdd,
		Arguments: map[string]any{"product_id": 12, "quantity": 1},
		Status:    "success",
		Metadata:  domain.Metadata{WriteOperation: true},
	})
	if err != nil {
		t.Fatalf("RecordToolCall with canceled request: %v", err)
	}
	if model.calls != 1 || auditRPC.calls != 1 {
		t.Fatalf("record attempts model=%d audit=%d, want both 1", model.calls, auditRPC.calls)
	}
}

func TestRecorderBoundsPersistedErrorMessage(t *testing.T) {
	model := &fakeToolCallModel{}
	recorder := NewRecorder(model, nil)

	err := recorder.RecordToolCall(context.Background(), tools.ToolCallRecord{
		UserID:       42,
		ToolName:     domain.ToolProductDetail,
		Arguments:    map[string]any{"product_id": 12},
		Status:       "failed",
		ErrorMessage: strings.Repeat("错", 600),
	})
	if err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	if got := len([]rune(model.row.ErrorMessage)); got > 512 {
		t.Fatalf("persisted error length = %d, want <= 512 runes", got)
	}
}

type fakeToolCallModel struct {
	row          *aitoolcalls.AiToolCalls
	calls        int
	err          error
	honorContext bool
}

func (f *fakeToolCallModel) Insert(ctx context.Context, row *aitoolcalls.AiToolCalls) (sql.Result, error) {
	f.calls++
	f.row = row
	if f.honorContext && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, f.err
}

type fakeAuditRPC struct {
	req          *auditclient.CreateAuditLogReq
	resp         *auditclient.CreateAuditLogRes
	calls        int
	err          error
	honorContext bool
}

func (f *fakeAuditRPC) CreateAuditLog(ctx context.Context, req *auditclient.CreateAuditLogReq, _ ...grpc.CallOption) (*auditclient.CreateAuditLogRes, error) {
	f.calls++
	f.req = req
	if f.honorContext && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.resp, f.err
}

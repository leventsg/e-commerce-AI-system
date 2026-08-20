package tool_calls

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestFindRecentSuccessfulToolCallsQueriesByUserConversationAndSuccess(t *testing.T) {
	model, mock, cleanup := newToolCallsTestModel(t)
	defer cleanup()
	createdAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(toolCallColumns()).
		AddRow(uint64(2), "conv-1", "call-2", uint64(42), "cart_add", `{"product_id":12}`, `{"cart_item_id":8}`, "success", "", int64(12), createdAt).
		AddRow(uint64(1), "conv-1", "call-1", uint64(42), "cart_list", `{}`, `{"items":[]}`, "success", "", int64(10), createdAt.Add(-time.Minute))
	mock.ExpectQuery("select "+aiToolCallsRows+" from `ai_tool_calls` where `user_id` = ? and `conversation_id` = ? and `status` = ? order by `id` desc limit ?").
		WithArgs(uint64(42), "conv-1", "success", 5).
		WillReturnRows(rows)

	got, err := model.FindRecentSuccessfulToolCalls(context.Background(), 42, "conv-1", 5)
	if err != nil {
		t.Fatalf("FindRecentSuccessfulToolCalls() error = %v", err)
	}
	if len(got) != 2 || got[0].ToolCallId != "call-2" || got[1].ToolCallId != "call-1" {
		t.Fatalf("rows = %+v", got)
	}
}

func TestFindToolCallByCallIDRequiresUserAndConversation(t *testing.T) {
	model, mock, cleanup := newToolCallsTestModel(t)
	defer cleanup()
	createdAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(toolCallColumns()).
		AddRow(uint64(9), "conv-1", "call-9", uint64(42), "order_get", `{"order_id":"order-1"}`, `{"order_id":"order-1"}`, "success", "", int64(21), createdAt)
	mock.ExpectQuery("select "+aiToolCallsRows+" from `ai_tool_calls` where `user_id` = ? and `conversation_id` = ? and `tool_call_id` = ? limit 1").
		WithArgs(uint64(42), "conv-1", "call-9").
		WillReturnRows(rows)

	got, err := model.FindToolCallByCallID(context.Background(), 42, "conv-1", "call-9")
	if err != nil {
		t.Fatalf("FindToolCallByCallID() error = %v", err)
	}
	if got.Id != 9 || got.ToolCallId != "call-9" || got.UserId != 42 || got.ConversationId != "conv-1" {
		t.Fatalf("row = %+v", got)
	}
}

func TestFindToolCallByCallIDNotFound(t *testing.T) {
	model, mock, cleanup := newToolCallsTestModel(t)
	defer cleanup()
	mock.ExpectQuery("select "+aiToolCallsRows+" from `ai_tool_calls` where `user_id` = ? and `conversation_id` = ? and `tool_call_id` = ? limit 1").
		WithArgs(uint64(42), "conv-1", "missing").
		WillReturnError(sql.ErrNoRows)

	if _, err := model.FindToolCallByCallID(context.Background(), 42, "conv-1", "missing"); err != ErrNotFound {
		t.Fatalf("FindToolCallByCallID() error = %v, want ErrNotFound", err)
	}
}

func toolCallColumns() []string {
	return []string{"id", "conversation_id", "tool_call_id", "user_id", "tool_name", "arguments", "result", "status", "error_message", "latency_ms", "created_at"}
}

func newToolCallsTestModel(t *testing.T) (*customAiToolCallsModel, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	model := &customAiToolCallsModel{
		defaultAiToolCallsModel: &defaultAiToolCallsModel{
			CachedConn: sqlc.NewConnWithCache(sqlx.NewSqlConnFromDB(db), noopToolCallCache{}),
			table:      "`ai_tool_calls`",
		},
	}
	return model, mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
		_ = db.Close()
	}
}

type noopToolCallCache struct{}

func (noopToolCallCache) Del(...string) error                            { return nil }
func (noopToolCallCache) DelCtx(context.Context, ...string) error        { return nil }
func (noopToolCallCache) Get(string, any) error                          { return sql.ErrNoRows }
func (noopToolCallCache) GetCtx(context.Context, string, any) error      { return sql.ErrNoRows }
func (noopToolCallCache) IsNotFound(err error) bool                      { return errors.Is(err, sql.ErrNoRows) }
func (noopToolCallCache) Set(string, any) error                          { return nil }
func (noopToolCallCache) SetCtx(context.Context, string, any) error      { return nil }
func (noopToolCallCache) SetWithExpire(string, any, time.Duration) error { return nil }
func (noopToolCallCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error {
	return nil
}
func (noopToolCallCache) Take(any, string, func(any) error) error                     { return nil }
func (noopToolCallCache) TakeCtx(context.Context, any, string, func(any) error) error { return nil }
func (noopToolCallCache) TakeWithExpire(any, string, func(any, time.Duration) error) error {
	return nil
}
func (noopToolCallCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return nil
}

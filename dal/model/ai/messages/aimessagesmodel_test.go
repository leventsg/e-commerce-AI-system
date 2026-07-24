package messages

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

func TestInsertBatchEmptyDoesNotExecuteSQL(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()

	if err := model.InsertBatch(context.Background(), nil); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL: %v", err)
	}
}

func TestInsertBatchRejectsNilMessageWithoutExecutingSQL(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()

	err := model.InsertBatch(context.Background(), []*AiMessages{nil})
	if !errors.Is(err, ErrNilBatchMessage) {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL: %v", err)
	}
}

func TestInsertBatchExecutesOneMultiValueInsert(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	now := time.Now()
	messages := []*AiMessages{
		{Id: "msg-1", ConversationId: "conv-1", UserId: 42, Role: "tool", Content: "result", Metadata: sql.NullString{String: `{"tool_name":"cart.add"}`, Valid: true}, CreatedAt: now},
		{Id: "msg-2", ConversationId: "conv-1", UserId: 42, Role: "assistant", Content: "done", CreatedAt: now},
	}
	query := "insert into `ai_messages` (`id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`) values (?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?)"
	mock.ExpectExec(query).WithArgs(
		"msg-1", "conv-1", uint64(42), "tool", "result", messages[0].Metadata,
		"msg-2", "conv-1", uint64(42), "assistant", "done", messages[1].Metadata,
	).WillReturnResult(sqlmock.NewResult(0, 2))

	if err := model.InsertBatch(context.Background(), messages); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInsertBatchReturnsDatabaseError(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	dbErr := errors.New("database unavailable")
	mock.ExpectExec("insert into `ai_messages` (`id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`) values (?, ?, ?, ?, ?, ?)").
		WithArgs("msg-1", "conv-1", uint64(42), "assistant", "hello", sql.NullString{}).
		WillReturnError(dbErr)

	err := model.InsertBatch(context.Background(), []*AiMessages{{Id: "msg-1", ConversationId: "conv-1", UserId: 42, Role: "assistant", Content: "hello"}})
	if !errors.Is(err, dbErr) {
		t.Fatalf("error=%v, want %v", err, dbErr)
	}
}

func TestFindRecentContextMessagesScopesByUserConversationAndExcludesTools(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	newer := time.Date(2026, 7, 24, 10, 1, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)
	query := "select " + aiMessagesRows + " from `ai_messages` where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?) order by `created_at` desc, `id` desc limit ?"
	rows := sqlmock.NewRows([]string{"id", "conversation_id", "user_id", "role", "content", "metadata", "created_at"}).
		AddRow("m2", "conv-1", uint64(42), "assistant", "newer", nil, newer).
		AddRow("m1", "conv-1", uint64(42), "user", "older", nil, older)
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "conv-1", "user", "assistant", 21).
		WillReturnRows(rows)

	messages, err := model.FindRecentContextMessages(context.Background(), 42, "conv-1", 21)
	if err != nil {
		t.Fatalf("FindRecentContextMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Id != "m1" || messages[1].Id != "m2" {
		t.Fatalf("messages = %+v", messages)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func newBatchTestModel(t *testing.T) (*customAiMessagesModel, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	model := &customAiMessagesModel{defaultAiMessagesModel: &defaultAiMessagesModel{
		CachedConn: sqlc.NewConnWithCache(sqlx.NewSqlConnFromDB(db), noopCache{}),
		table:      "`ai_messages`",
	}}
	return model, mock, func() { _ = db.Close() }
}

type noopCache struct{}

func (noopCache) Del(...string) error                                                { return nil }
func (noopCache) DelCtx(context.Context, ...string) error                            { return nil }
func (noopCache) Get(string, any) error                                              { return sql.ErrNoRows }
func (noopCache) GetCtx(context.Context, string, any) error                          { return sql.ErrNoRows }
func (noopCache) IsNotFound(err error) bool                                          { return errors.Is(err, sql.ErrNoRows) }
func (noopCache) Set(string, any) error                                              { return nil }
func (noopCache) SetCtx(context.Context, string, any) error                          { return nil }
func (noopCache) SetWithExpire(string, any, time.Duration) error                     { return nil }
func (noopCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error { return nil }
func (noopCache) Take(any, string, func(any) error) error                            { return nil }
func (noopCache) TakeCtx(context.Context, any, string, func(any) error) error        { return nil }
func (noopCache) TakeWithExpire(any, string, func(any, time.Duration) error) error   { return nil }
func (noopCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return nil
}

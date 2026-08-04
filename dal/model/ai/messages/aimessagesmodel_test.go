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
		{MsgId: "msg-1", ConversationId: "conv-1", UserId: 42, Role: "tool", Content: "result", Metadata: sql.NullString{String: `{"tool_name":"cart_add"}`, Valid: true}, ClientMessageId: sql.NullString{String: "client-1", Valid: true}, CreatedAt: now},
		{MsgId: "msg-2", ConversationId: "conv-1", UserId: 42, Role: "assistant", Content: "done", ClientMessageId: sql.NullString{String: "client-1", Valid: true}, CreatedAt: now},
	}
	query := "insert into `ai_messages` (`msg_id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`, `client_message_id`) values (?, ?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?, ?)"
	mock.ExpectExec(query).WithArgs(
		"msg-1", "conv-1", uint64(42), "tool", "result", messages[0].Metadata, messages[0].ClientMessageId,
		"msg-2", "conv-1", uint64(42), "assistant", "done", messages[1].Metadata, messages[1].ClientMessageId,
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
	mock.ExpectExec("insert into `ai_messages` (`msg_id`, `conversation_id`, `user_id`, `role`, `content`, `metadata`, `client_message_id`) values (?, ?, ?, ?, ?, ?, ?)").
		WithArgs("msg-1", "conv-1", uint64(42), "assistant", "hello", sql.NullString{}, sql.NullString{}).
		WillReturnError(dbErr)

	err := model.InsertBatch(context.Background(), []*AiMessages{{MsgId: "msg-1", ConversationId: "conv-1", UserId: 42, Role: "assistant", Content: "hello"}})
	if !errors.Is(err, dbErr) {
		t.Fatalf("error=%v, want %v", err, dbErr)
	}
}

func TestFindRecentContextMessagesScopesByUserConversationAndExcludesTools(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	newer := time.Date(2026, 7, 24, 10, 1, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)
	query := "select " + aiMessagesRows + " from `ai_messages` where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?) order by `id` desc limit ?"
	rows := sqlmock.NewRows([]string{"id", "msg_id", "conversation_id", "user_id", "role", "content", "metadata", "client_message_id", "dedupe_client_message_id", "created_at"}).
		AddRow(uint64(2), "m2", "conv-1", uint64(42), "assistant", "newer", nil, "client-1", nil, newer).
		AddRow(uint64(1), "m1", "conv-1", uint64(42), "user", "older", nil, "client-1", "client-1", older)
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "conv-1", "user", "assistant", 21).
		WillReturnRows(rows)

	messages, err := model.FindRecentContextMessages(context.Background(), 42, "conv-1", 21)
	if err != nil {
		t.Fatalf("FindRecentContextMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].MsgId != "m1" || messages[1].MsgId != "m2" {
		t.Fatalf("messages = %+v", messages)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCountUnsummarizedContextMessagesUsesSameWatermarkAndRoleScope(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	query := "select count(1) from `ai_messages` where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?) and `id` > (select `id` from `ai_messages` where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? limit 1)"
	rows := sqlmock.NewRows([]string{"count(1)"}).AddRow(int64(17))
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "conv-1", "user", "assistant", "m010", uint64(42), "conv-1").
		WillReturnRows(rows)

	count, err := model.CountUnsummarizedContextMessages(context.Background(), 42, "conv-1", "2026-07-24 10:00:00.000", "m010")
	if err != nil {
		t.Fatalf("CountUnsummarizedContextMessages() error = %v", err)
	}
	if count != 17 {
		t.Fatalf("count = %d, want 17", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestFindRecentUnsummarizedContextMessagesReturnsOldestFirstRecentWindow(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	newer := time.Date(2026, 7, 24, 10, 2, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)
	query := "select " + aiMessagesRows + " from `ai_messages` where `user_id` = ? and `conversation_id` = ? and `role` in (?, ?) and `id` > (select `id` from `ai_messages` where `msg_id` = ? and `user_id` = ? and `conversation_id` = ? limit 1) order by `id` desc limit ?"
	rows := sqlmock.NewRows([]string{"id", "msg_id", "conversation_id", "user_id", "role", "content", "metadata", "client_message_id", "dedupe_client_message_id", "created_at"}).
		AddRow(uint64(3), "m3", "conv-1", uint64(42), "assistant", "newer", nil, "client-1", nil, newer).
		AddRow(uint64(2), "m2", "conv-1", uint64(42), "user", "older", nil, "client-1", "client-1", older)
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "conv-1", "user", "assistant", "m001", uint64(42), "conv-1", 20).
		WillReturnRows(rows)

	messages, err := model.FindRecentUnsummarizedContextMessages(context.Background(), 42, "conv-1", "2026-07-24 10:00:00.000", "m001", 20)
	if err != nil {
		t.Fatalf("FindRecentUnsummarizedContextMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].MsgId != "m2" || messages[1].MsgId != "m3" {
		t.Fatalf("messages = %+v", messages)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestFindUserMessageByClientMessageIDScopesByUserAndRole(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	createdAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	query := "select " + aiMessagesRows + " from `ai_messages` where `user_id` = ? and `client_message_id` = ? and `role` = ? limit 1"
	rows := sqlmock.NewRows([]string{"id", "msg_id", "conversation_id", "user_id", "role", "content", "metadata", "client_message_id", "dedupe_client_message_id", "created_at"}).
		AddRow(uint64(7), "msg-user", "conv-1", uint64(42), "user", "你好", nil, "client-1", "client-1", createdAt)
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "client-1", "user").
		WillReturnRows(rows)

	message, err := model.FindUserMessageByClientMessageID(context.Background(), 42, "client-1")
	if err != nil {
		t.Fatalf("FindUserMessageByClientMessageID() error = %v", err)
	}
	if message.MsgId != "msg-user" || message.Id != 7 || message.ConversationId != "conv-1" {
		t.Fatalf("message = %+v", message)
	}
}

func TestFindAssistantMessagesByClientMessageIDScopesAndOrdersById(t *testing.T) {
	model, mock, cleanup := newBatchTestModel(t)
	defer cleanup()
	createdAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	query := "select " + aiMessagesRows + " from `ai_messages` where `user_id` = ? and `conversation_id` = ? and `client_message_id` = ? and `role` = ? order by `id` asc"
	rows := sqlmock.NewRows([]string{"id", "msg_id", "conversation_id", "user_id", "role", "content", "metadata", "client_message_id", "dedupe_client_message_id", "created_at"}).
		AddRow(uint64(8), "assistant-1", "conv-1", uint64(42), "assistant", "旧回复 1", nil, "client-1", nil, createdAt).
		AddRow(uint64(9), "assistant-2", "conv-1", uint64(42), "assistant", "旧回复 2", nil, "client-1", nil, createdAt)
	mock.ExpectQuery(query).
		WithArgs(uint64(42), "conv-1", "client-1", "assistant").
		WillReturnRows(rows)

	messages, err := model.FindAssistantMessagesByClientMessageID(context.Background(), 42, "conv-1", "client-1")
	if err != nil {
		t.Fatalf("FindAssistantMessagesByClientMessageID() error = %v", err)
	}
	if len(messages) != 2 || messages[0].MsgId != "assistant-1" || messages[1].MsgId != "assistant-2" {
		t.Fatalf("messages = %+v", messages)
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

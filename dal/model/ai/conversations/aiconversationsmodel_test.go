package conversations

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

type noopCache struct{}

func (noopCache) Del(...string) error                       { return nil }
func (noopCache) DelCtx(context.Context, ...string) error   { return nil }
func (noopCache) Get(string, any) error                     { return sql.ErrNoRows }
func (noopCache) GetCtx(context.Context, string, any) error { return sql.ErrNoRows }
func (noopCache) IsNotFound(err error) bool                 { return errors.Is(err, sql.ErrNoRows) }
func (noopCache) Set(string, any) error                     { return nil }
func (noopCache) SetCtx(context.Context, string, any) error { return nil }
func (noopCache) SetWithExpire(string, any, time.Duration) error {
	return nil
}
func (noopCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error {
	return nil
}
func (noopCache) Take(any, string, func(any) error) error {
	return nil
}
func (noopCache) TakeCtx(context.Context, any, string, func(any) error) error {
	return nil
}
func (noopCache) TakeWithExpire(any, string, func(any, time.Duration) error) error {
	return nil
}
func (noopCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return nil
}

func newConversationsTestModel(t *testing.T) (*customAiConversationsModel, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	model := &customAiConversationsModel{defaultAiConversationsModel: &defaultAiConversationsModel{
		CachedConn: sqlc.NewConnWithCache(sqlx.NewSqlConnFromDB(db), noopCache{}),
		table:      "`ai_conversations`",
	}}
	return model, mock, func() { _ = db.Close() }
}

func TestFindByUserWithStatsReturnsPreviewAndStats(t *testing.T) {
	model, mock, cleanup := newConversationsTestModel(t)
	defer cleanup()
	created := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	lastMessageAt := time.Date(2026, 8, 20, 15, 6, 37, 0, time.UTC)
	cols := []string{
		"id", "user_id", "title", "status", "created_at", "updated_at",
		"last_message_at", "last_message_preview", "message_count",
	}
	rows := sqlmock.NewRows(cols).
		AddRow("conv-1", uint64(42), "订单咨询", "active", created, created, lastMessageAt, "你的订单已送达", int64(6)).
		AddRow("conv-2", uint64(42), "新会话", "active", created.Add(-time.Hour), created.Add(-time.Hour), nil, "", int64(0))
	query := "select c.id, c.user_id, c.title, c.status, c.created_at, c.updated_at, " +
		"coalesce((select max(m.created_at) from `ai_messages` m where m.conversation_id = c.id and m.user_id = ?), c.created_at) as last_message_at, " +
		"coalesce((select m.content from `ai_messages` m where m.conversation_id = c.id and m.user_id = ? and m.role in (?, ?) order by m.id desc limit 1), '') as last_message_preview, " +
		"(select count(1) from `ai_messages` m where m.conversation_id = c.id and m.user_id = ?) as message_count " +
		"from `ai_conversations` c where c.user_id = ? " +
		"order by last_message_at desc, c.created_at desc limit ? offset ?"
	mock.ExpectQuery(query).WithArgs(uint64(42), uint64(42), "user", "assistant", uint64(42), uint64(42), 20, 40).WillReturnRows(rows)

	items, err := model.FindByUserWithStats(context.Background(), 42, 20, 40)
	if err != nil {
		t.Fatalf("FindByUserWithStats: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	first := items[0]
	if first.Id != "conv-1" || first.Title != "订单咨询" || !first.LastMessageAt.Valid || first.LastMessageAt.Time != lastMessageAt || first.LastMessagePreview != "你的订单已送达" || first.MessageCount != 6 {
		t.Fatalf("first item = %+v", first)
	}
	if items[1].Id != "conv-2" || items[1].LastMessageAt.Valid || items[1].MessageCount != 0 {
		t.Fatalf("second item = %+v", items[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCountByUser(t *testing.T) {
	model, mock, cleanup := newConversationsTestModel(t)
	defer cleanup()
	query := "select count(1) from `ai_conversations` where `user_id` = ?"
	mock.ExpectQuery(query).WithArgs(uint64(42)).WillReturnRows(sqlmock.NewRows([]string{"count(1)"}).AddRow(int64(3)))

	count, err := model.CountByUser(context.Background(), 42)
	if err != nil {
		t.Fatalf("CountByUser: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

package memory

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestSQLEventStoreListsRecentUserMemoryEvents(t *testing.T) {
	store, mock, cleanup := newEventStoreTest(t)
	defer cleanup()
	query := "select `user_id`, `type`, `event_date`, `summary`, `keywords` from `ai_user_memory_events` where `user_id` = ? order by `event_date` desc, `id` desc limit ?"
	eventDate := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"user_id", "type", "event_date", "summary", "keywords"}).
		AddRow(uint64(42), "milestone", eventDate, "完成 MemoryProvider 重构", `["memory","context"]`)
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(uint64(42), 3).WillReturnRows(rows)

	events, err := store.ListRecentUserMemoryEvents(context.Background(), 42, 3)
	if err != nil {
		t.Fatalf("ListRecentUserMemoryEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].UserID != 42 || events[0].Keywords[0] != "memory" {
		t.Fatalf("events = %+v", events)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSQLEventStoreSearchesByTrustedUserAndKeyword(t *testing.T) {
	store, mock, cleanup := newEventStoreTest(t)
	defer cleanup()
	query := "select `user_id`, `type`, `event_date`, `summary`, `keywords` from `ai_user_memory_events` where `user_id` = ? and `status` = ? and (`summary` like ? or `keywords` like ?) order by `event_date` desc, `id` desc limit ?"
	eventDate := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"user_id", "type", "event_date", "summary", "keywords"}).
		AddRow(uint64(42), "event", eventDate, "用户喜欢轻薄手机", `["手机"]`)
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(uint64(42), "active", "%手机%", "%手机%", 5).WillReturnRows(rows)

	events, err := store.SearchUserMemoryEvents(context.Background(), UserMemoryEventQuery{
		UserID:   42,
		Keywords: []string{"手机"},
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("SearchUserMemoryEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Summary != "用户喜欢轻薄手机" {
		t.Fatalf("events = %+v", events)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSQLEventStoreSavesUserMemoryEvents(t *testing.T) {
	store, mock, cleanup := newEventStoreTest(t)
	defer cleanup()
	query := "insert into `ai_user_memory_events` (`id`, `user_id`, `type`, `event_date`, `summary`, `keywords`, `status`) values (?, ?, ?, ?, ?, ?, ?)"
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(sqlmock.AnyArg(), uint64(42), "event", "2026-08-14", "用户偏好轻薄手机", jsonArrayArg(`["手机","轻薄"]`), "active").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.SaveUserMemoryEvents(context.Background(), 42, []Event{{
		Type:      "event",
		EventDate: "2026-08-14",
		Summary:   "用户偏好轻薄手机",
		Keywords:  []string{"手机", "轻薄"},
	}})
	if err != nil {
		t.Fatalf("SaveUserMemoryEvents() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func newEventStoreTest(t *testing.T) (*SQLEventStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	return NewSQLEventStore(sqlx.NewSqlConnFromDB(db)), mock, func() { _ = db.Close() }
}

type jsonArrayArg string

func (j jsonArrayArg) Match(value driver.Value) bool {
	raw, ok := value.(string)
	return ok && strings.TrimSpace(raw) == string(j)
}

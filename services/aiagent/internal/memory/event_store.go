package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	defaultEventSearchLimit = 10
	maxEventSearchLimit     = 30
)

type SQLEventStore struct {
	conn sqlx.SqlConn
}

func NewSQLEventStore(conn sqlx.SqlConn) *SQLEventStore {
	return &SQLEventStore{conn: conn}
}

func (s *SQLEventStore) ListRecentUserMemoryEvents(ctx context.Context, userID uint64, limit int) ([]Event, error) {
	if s == nil || s.conn == nil || userID == 0 {
		return nil, nil
	}
	limit = normalizeEventLimit(limit)
	var rows []eventRow
	query := "select `user_id`, `type`, `event_date`, `summary`, `keywords` from `ai_user_memory_events` where `user_id` = ? order by `event_date` desc, `id` desc limit ?"
	if err := s.conn.QueryRowsCtx(ctx, &rows, query, userID, limit); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return eventsFromRows(rows), nil
}

func (s *SQLEventStore) SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error) {
	if s == nil || s.conn == nil || query.UserID == 0 {
		return nil, nil
	}
	limit := normalizeEventLimit(query.Limit)
	sqlText := "select `user_id`, `type`, `event_date`, `summary`, `keywords` from `ai_user_memory_events` where `user_id` = ? and `status` = ?"
	args := []any{query.UserID, "active"}
	if query.Type != "" {
		sqlText += " and `type` = ?"
		args = append(args, query.Type)
	}
	for _, keyword := range compactKeywords(query.Keywords) {
		sqlText += " and (`summary` like ? or `keywords` like ?)"
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}
	if query.Since != "" {
		sqlText += " and `event_date` >= ?"
		args = append(args, query.Since)
	}
	if query.Until != "" {
		sqlText += " and `event_date` <= ?"
		args = append(args, query.Until)
	}
	sqlText += " order by `event_date` desc, `id` desc limit ?"
	args = append(args, limit)

	var rows []eventRow
	if err := s.conn.QueryRowsCtx(ctx, &rows, sqlText, args...); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return eventsFromRows(rows), nil
}

type eventRow struct {
	UserID    uint64    `db:"user_id"`
	Type      string    `db:"type"`
	EventDate time.Time `db:"event_date"`
	Summary   string    `db:"summary"`
	Keywords  string    `db:"keywords"`
}

func eventsFromRows(rows []eventRow) []Event {
	result := make([]Event, 0, len(rows))
	for _, row := range rows {
		var keywords []string
		_ = json.Unmarshal([]byte(row.Keywords), &keywords)
		result = append(result, Event{
			UserID:    row.UserID,
			Type:      row.Type,
			EventDate: row.EventDate.Format("2006-01-02"),
			Summary:   row.Summary,
			Keywords:  keywords,
		})
	}
	return result
}

func normalizeEventLimit(limit int) int {
	if limit <= 0 {
		return defaultEventSearchLimit
	}
	if limit > maxEventSearchLimit {
		return maxEventSearchLimit
	}
	return limit
}

func compactKeywords(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

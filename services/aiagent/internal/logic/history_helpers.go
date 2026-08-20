package logic

import (
	"database/sql"
	"time"
)

const (
	defaultHistoryPage     = 1
	defaultHistoryPageSize = 50
	maxHistoryPageSize     = 200
)

// normalizeHistoryPage 归一化历史列表分页参数：页码最小为 1，页大小默认 50、上限 200。
func normalizeHistoryPage(page, pageSize int64) (int, int) {
	if page < 1 {
		page = defaultHistoryPage
	}
	if pageSize <= 0 {
		pageSize = defaultHistoryPageSize
	}
	if pageSize > maxHistoryPageSize {
		pageSize = maxHistoryPageSize
	}
	return int(page), int(pageSize)
}

// historyTime 返回会话最后活跃时间（RFC3339）；没有消息时回退到会话创建时间。
func historyTime(t sql.NullTime, fallback time.Time) string {
	if t.Valid {
		return t.Time.Format(time.RFC3339)
	}
	return fallback.Format(time.RFC3339)
}

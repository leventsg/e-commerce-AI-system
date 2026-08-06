package agent_runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlc"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type AiAgentRuns struct {
	RunId          string         `db:"run_id"`
	ConversationId string         `db:"conversation_id"`
	UserId         uint64         `db:"user_id"`
	Status         string         `db:"status"`
	CheckpointId   string         `db:"checkpoint_id"`
	CheckpointBlob []byte         `db:"checkpoint_blob"`
	TaskState      sql.NullString `db:"task_state"`
	IdempotencyKey string         `db:"idempotency_key"`
	ExpiresAt      time.Time      `db:"expires_at"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`
}

type AiAgentRunsModel interface {
	FindOneByCheckpointID(ctx context.Context, checkpointID string) (*AiAgentRuns, error)
	UpsertCheckpoint(ctx context.Context, row *AiAgentRuns) error
	DeleteByCheckpointID(ctx context.Context, checkpointID string) error
}

type customAiAgentRunsModel struct {
	conn  sqlx.SqlConn
	table string
}

func NewAiAgentRunsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) AiAgentRunsModel {
	return &customAiAgentRunsModel{conn: conn, table: "`ai_agent_runs`"}
}

func (m *customAiAgentRunsModel) FindOneByCheckpointID(ctx context.Context, checkpointID string) (*AiAgentRuns, error) {
	var row AiAgentRuns
	query := fmt.Sprintf("select `run_id`, `conversation_id`, `user_id`, `status`, `checkpoint_id`, `checkpoint_blob`, `task_state`, `idempotency_key`, `expires_at`, `created_at`, `updated_at` from %s where `checkpoint_id` = ? limit 1", m.table)
	err := m.conn.QueryRowCtx(ctx, &row, query, checkpointID)
	switch {
	case err == nil:
		return &row, nil
	case errors.Is(err, sqlc.ErrNotFound), errors.Is(err, sqlx.ErrNotFound):
		return nil, ErrNotFound
	default:
		return nil, err
	}
}

func (m *customAiAgentRunsModel) UpsertCheckpoint(ctx context.Context, row *AiAgentRuns) error {
	if row == nil {
		return errors.New("agent run is nil")
	}
	query := fmt.Sprintf("insert into %s (`run_id`, `conversation_id`, `user_id`, `status`, `checkpoint_id`, `checkpoint_blob`, `task_state`, `idempotency_key`, `expires_at`) values (?, ?, ?, ?, ?, ?, ?, ?, ?) on duplicate key update `conversation_id` = values(`conversation_id`), `user_id` = values(`user_id`), `status` = values(`status`), `checkpoint_blob` = values(`checkpoint_blob`), `task_state` = values(`task_state`), `idempotency_key` = values(`idempotency_key`), `expires_at` = values(`expires_at`)", m.table)
	_, err := m.conn.ExecCtx(ctx, query, row.RunId, row.ConversationId, row.UserId, row.Status, row.CheckpointId, row.CheckpointBlob, row.TaskState, row.IdempotencyKey, row.ExpiresAt)
	return err
}

func (m *customAiAgentRunsModel) DeleteByCheckpointID(ctx context.Context, checkpointID string) error {
	query := fmt.Sprintf("delete from %s where `checkpoint_id` = ?", m.table)
	_, err := m.conn.ExecCtx(ctx, query, checkpointID)
	return err
}

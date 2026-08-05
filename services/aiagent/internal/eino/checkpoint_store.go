package eino

import (
	"context"
	"database/sql"
	"encoding/base64"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	aiagentruns "github.com/leventsg/e-commerce-AI-system/dal/model/ai/agent_runs"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type memoryCheckpointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMemoryCheckpointStore() *memoryCheckpointStore {
	return &memoryCheckpointStore{data: make(map[string][]byte)}
}

func (s *memoryCheckpointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), data...), true, nil
}

func (s *memoryCheckpointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[checkPointID] = append([]byte(nil), checkPoint...)
	return nil
}

func (s *memoryCheckpointStore) Delete(_ context.Context, checkPointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, checkPointID)
	return nil
}

type persistentCheckpointStore struct {
	redis *redis.Redis
	model aiagentruns.AiAgentRunsModel
	ttl   time.Duration
}

func NewPersistentCheckpointStore(redisClient *redis.Redis, model aiagentruns.AiAgentRunsModel, ttl time.Duration) adk.CheckPointStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &persistentCheckpointStore{redis: redisClient, model: model, ttl: ttl}
}

func (s *persistentCheckpointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	key := checkpointRedisKey(checkPointID)
	if s.redis != nil {
		value, err := s.redis.GetCtx(ctx, key)
		if err == nil && strings.TrimSpace(value) != "" {
			data, decodeErr := base64.StdEncoding.DecodeString(value)
			if decodeErr == nil {
				return data, true, nil
			}
			logx.WithContext(ctx).Errorw("decode ai agent checkpoint cache failed", logx.Field("checkpoint_id", checkPointID), logx.Field("err", decodeErr))
		}
	}
	if s.model == nil {
		return nil, false, nil
	}
	row, err := s.model.FindOneByCheckpointID(ctx, checkPointID)
	if err != nil {
		if err == aiagentruns.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	data := append([]byte(nil), row.CheckpointBlob...)
	s.setRedis(ctx, key, data)
	return data, true, nil
}

func (s *persistentCheckpointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	key := checkpointRedisKey(checkPointID)
	s.setRedis(ctx, key, checkPoint)
	if s.model == nil {
		return nil
	}
	execution, _ := aitools.ToolExecutionFromContext(ctx)
	runID := strings.TrimSpace(execution.RunID)
	if runID == "" {
		runID = checkPointID
	}
	return s.model.UpsertCheckpoint(ctx, &aiagentruns.AiAgentRuns{
		RunId:          runID,
		ConversationId: execution.ConversationID,
		UserId:         execution.UserID,
		Status:         "interrupted",
		CheckpointId:   checkPointID,
		CheckpointBlob: append([]byte(nil), checkPoint...),
		TaskState:      sql.NullString{},
		IdempotencyKey: runID,
		ExpiresAt:      time.Now().Add(s.ttl),
	})
}

func (s *persistentCheckpointStore) Delete(ctx context.Context, checkPointID string) error {
	if s.redis != nil {
		_, _ = s.redis.DelCtx(ctx, checkpointRedisKey(checkPointID))
	}
	if s.model == nil {
		return nil
	}
	return s.model.DeleteByCheckpointID(ctx, checkPointID)
}

func (s *persistentCheckpointStore) setRedis(ctx context.Context, key string, data []byte) {
	if s.redis == nil {
		return
	}
	seconds := int(math.Ceil(s.ttl.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	value := base64.StdEncoding.EncodeToString(data)
	if err := s.redis.SetexCtx(ctx, key, value, seconds); err != nil {
		logx.WithContext(ctx).Errorw("save ai agent checkpoint cache failed", logx.Field("key", key), logx.Field("err", err))
	}
}

func checkpointRedisKey(checkPointID string) string {
	return "ai:agent:checkpoint:" + checkPointID
}

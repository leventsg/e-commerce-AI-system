package confirmation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusExpired  = "expired"
	StatusExecuted = "executed"
	StatusFailed   = "failed"

	defaultConfirmationTTL = 300 * time.Second
	defaultLockTTL         = 5 * time.Second
	lockReleaseTimeout     = time.Second
	maxSummaryRunes        = 512
	confirmationLockPrefix = "ai:confirmation:lock:"
)

var (
	ErrInvalidConfirmation          = errors.New("invalid confirmation request")
	ErrConfirmationToolNotAllowed   = errors.New("tool does not allow confirmation")
	ErrConfirmationNotFound         = errors.New("confirmation not found")
	ErrConfirmationForbidden        = errors.New("confirmation access forbidden")
	ErrConfirmationBusy             = errors.New("confirmation is being processed")
	ErrConfirmationExpired          = errors.New("confirmation expired")
	ErrConfirmationRejected         = errors.New("confirmation rejected")
	ErrConfirmationAlreadyProcessed = errors.New("confirmation already processed")
)

var sensitiveArgumentKeys = []string{
	"user_id",
	"token",
	"access_token",
	"refresh_token",
	"session_id",
	"auth",
	"authorization",
	"cookie",
	"jwt",
}

type Model interface {
	Insert(ctx context.Context, data *aiconfirmations.AiConfirmations) (sql.Result, error)
	FindOneUncached(ctx context.Context, id string) (*aiconfirmations.AiConfirmations, error)
	ResolvePending(ctx context.Context, id string, userID uint64, nextStatus string, now time.Time) (bool, error)
	ExpirePending(ctx context.Context, id string, userID uint64, now time.Time) (bool, error)
	CompleteApproved(ctx context.Context, id string, userID uint64, nextStatus string, executedAt time.Time) (bool, error)
	BindResumeTarget(ctx context.Context, id string, userID uint64, runID string, checkpointID string, interruptID string) (bool, error)
}

type MetadataRegistry interface {
	Metadata(name string) (domain.Metadata, error)
}

type CreateRequest struct {
	UserID         uint64
	ConversationID string
	ToolName       string
	Arguments      map[string]any
	Summary        string
	RunID          string
	CheckpointID   string
}

type DecisionRequest struct {
	UserID         uint64
	ConversationID string
	ConfirmationID string
	Approved       bool
}

type CompletionRequest struct {
	UserID         uint64
	ConversationID string
	ConfirmationID string
}

type ResumeTargetRequest struct {
	UserID         uint64
	ConversationID string
	ConfirmationID string
	RunID          string
	CheckpointID   string
	InterruptID    string
}

type Manager struct {
	model           Model
	registry        MetadataRegistry
	locker          Locker
	confirmationTTL time.Duration
	lockTTL         time.Duration
	clock           func() time.Time
	idGenerator     func() string
}

type Option func(*Manager)

func WithConfirmationTTL(ttl time.Duration) Option {
	return func(m *Manager) {
		if ttl > 0 {
			m.confirmationTTL = ttl
		}
	}
}

func WithLockTTL(ttl time.Duration) Option {
	return func(m *Manager) {
		if ttl > 0 {
			m.lockTTL = ttl
		}
	}
}

func WithClock(clock func() time.Time) Option {
	return func(m *Manager) {
		if clock != nil {
			m.clock = clock
		}
	}
}

func WithIDGenerator(generator func() string) Option {
	return func(m *Manager) {
		if generator != nil {
			m.idGenerator = generator
		}
	}
}

func NewManager(model Model, registry MetadataRegistry, locker Locker, opts ...Option) *Manager {
	manager := &Manager{
		model:           model,
		registry:        registry,
		locker:          locker,
		confirmationTTL: defaultConfirmationTTL,
		lockTTL:         defaultLockTTL,
		clock:           time.Now,
		idGenerator:     newConfirmationID,
	}
	for _, opt := range opts {
		opt(manager)
	}
	return manager
}

// Create 创建一个新的确认请求，存储在数据库中
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*domain.Confirmation, error) {
	if m.model == nil || m.registry == nil || req.UserID == 0 || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ToolName) == "" {
		return nil, ErrInvalidConfirmation
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" || utf8.RuneCountInString(summary) > maxSummaryRunes {
		return nil, fmt.Errorf("%w: summary is required and must not exceed %d characters", ErrInvalidConfirmation, maxSummaryRunes)
	}
	metadata, err := m.registry.Metadata(req.ToolName)
	// 校验工具是否属于高风险、需要确认、写操作的工具
	if err != nil || metadata.Risk != domain.RiskHigh || !metadata.RequireConfirmation || !metadata.WriteOperation {
		return nil, fmt.Errorf("%w: %s", ErrConfirmationToolNotAllowed, req.ToolName)
	}
	// 清理敏感参数
	arguments := argx.SanitizeMapKeys(req.Arguments, sensitiveArgumentKeys)
	if arguments == nil {
		arguments = map[string]any{}
	}
	argumentsJSON, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal arguments: %v", ErrInvalidConfirmation, err)
	}
	now := m.now()
	row := &aiconfirmations.AiConfirmations{
		Id:             m.idGenerator(),
		ConversationId: strings.TrimSpace(req.ConversationID),
		UserId:         req.UserID,
		ToolName:       metadata.Name,
		Arguments:      string(argumentsJSON),
		Summary:        summary,
		Status:         StatusPending,
		RunId:          strings.TrimSpace(req.RunID),
		CheckpointId:   strings.TrimSpace(req.CheckpointID),
		ExpiresAt:      now.Add(m.confirmationTTL),
	}
	if strings.TrimSpace(row.Id) == "" {
		return nil, fmt.Errorf("%w: confirmation id is empty", ErrInvalidConfirmation)
	}
	// 插入待处理的确认请求记录
	result, insertErr := m.model.Insert(ctx, row)
	if insertErr != nil && result == nil {
		return nil, insertErr
	}
	if insertErr != nil {
		logx.WithContext(ctx).Errorw("confirmation cache invalidation failed after committed insert",
			logx.Field("confirmation_id", row.Id), logx.Field("err", insertErr))
	}
	return confirmationFromRow(row)
}

// Decide 用户决策确认请求
func (m *Manager) Decide(ctx context.Context, req DecisionRequest) (*domain.Confirmation, error) {
	if req.UserID == 0 || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ConfirmationID) == "" {
		return nil, ErrInvalidConfirmation
	}
	return m.withLock(ctx, req.ConfirmationID, func() (*domain.Confirmation, error) {
		// 获取待处理的确认请求记录
		row, err := m.loadOwned(ctx, req.UserID, req.ConversationID, req.ConfirmationID)
		if err != nil {
			return nil, err
		}
		if row.Status != StatusPending {
			return nil, confirmationStateError(row.Status)
		}
		now := m.now()
		if !row.ExpiresAt.After(now) {
			// 已过期，更新状态为expired并返回错误
			expired, err := m.model.ExpirePending(ctx, row.Id, req.UserID, now)
			if err != nil {
				return nil, err
			}
			if expired {
				return nil, ErrConfirmationExpired
			}
			current, err := m.loadOwned(ctx, req.UserID, req.ConversationID, req.ConfirmationID)
			if err != nil {
				return nil, err
			}
			return nil, confirmationStateError(current.Status)
		}
		nextStatus := StatusRejected
		if req.Approved {
			nextStatus = StatusApproved
		}
		// 更新确认请求记录状态
		updated, err := m.model.ResolvePending(ctx, row.Id, req.UserID, nextStatus, now)
		if err != nil {
			return nil, err
		}
		if !updated {
			return nil, ErrConfirmationAlreadyProcessed
		}
		row.Status = nextStatus
		return confirmationFromRow(row)
	})
}

func (m *Manager) MarkExecuted(ctx context.Context, req CompletionRequest) (*domain.Confirmation, error) {
	return m.complete(ctx, req, StatusExecuted)
}

func (m *Manager) MarkFailed(ctx context.Context, req CompletionRequest) (*domain.Confirmation, error) {
	return m.complete(ctx, req, StatusFailed)
}

func (m *Manager) BindResumeTarget(ctx context.Context, req ResumeTargetRequest) (*domain.Confirmation, error) {
	if req.UserID == 0 || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ConfirmationID) == "" ||
		strings.TrimSpace(req.CheckpointID) == "" || strings.TrimSpace(req.InterruptID) == "" {
		return nil, ErrInvalidConfirmation
	}
	return m.withLock(ctx, req.ConfirmationID, func() (*domain.Confirmation, error) {
		row, err := m.loadOwned(ctx, req.UserID, req.ConversationID, req.ConfirmationID)
		if err != nil {
			return nil, err
		}
		if row.Status != StatusPending {
			return nil, confirmationStateError(row.Status)
		}
		updated, err := m.model.BindResumeTarget(ctx, row.Id, req.UserID, strings.TrimSpace(req.RunID), strings.TrimSpace(req.CheckpointID), strings.TrimSpace(req.InterruptID))
		if err != nil {
			return nil, err
		}
		if !updated {
			return nil, ErrConfirmationAlreadyProcessed
		}
		row.RunId = strings.TrimSpace(req.RunID)
		row.CheckpointId = strings.TrimSpace(req.CheckpointID)
		row.InterruptId = strings.TrimSpace(req.InterruptID)
		return confirmationFromRow(row)
	})
}

// complete 完成确认请求的处理，更新状态为执行成功或失败
func (m *Manager) complete(ctx context.Context, req CompletionRequest, nextStatus string) (*domain.Confirmation, error) {
	if req.UserID == 0 || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.ConfirmationID) == "" {
		return nil, ErrInvalidConfirmation
	}
	return m.withLock(ctx, req.ConfirmationID, func() (*domain.Confirmation, error) {
		row, err := m.loadOwned(ctx, req.UserID, req.ConversationID, req.ConfirmationID)
		if err != nil {
			return nil, err
		}
		if row.Status != StatusApproved {
			return nil, ErrConfirmationAlreadyProcessed
		}
		executedAt := m.now()
		updated, err := m.model.CompleteApproved(ctx, row.Id, req.UserID, nextStatus, executedAt)
		if err != nil {
			return nil, err
		}
		if !updated {
			return nil, ErrConfirmationAlreadyProcessed
		}
		row.Status = nextStatus
		row.ExecutedAt = sql.NullTime{Time: executedAt, Valid: true}
		return confirmationFromRow(row)
	})
}

func (m *Manager) withLock(ctx context.Context, confirmationID string, operation func() (*domain.Confirmation, error)) (*domain.Confirmation, error) {
	if m.locker == nil {
		return operation()
	}
	// 根据confirmationID尝试获取确认锁，防止并发处理同一个确认请求
	lock, acquired, err := m.locker.Acquire(ctx, confirmationLockPrefix+confirmationID, m.lockTTL)
	if err != nil {
		logx.WithContext(ctx).Errorw("confirmation redis lock unavailable; falling back to mysql cas",
			logx.Field("confirmation_id", confirmationID), logx.Field("err", err))
		return operation()
	}
	if !acquired {
		return nil, ErrConfirmationBusy
	}
	defer func() {
		// 设置锁超时为1s
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lockReleaseTimeout)
		defer cancel()
		if releaseErr := lock.Release(releaseCtx); releaseErr != nil {
			logx.WithContext(releaseCtx).Errorw("release confirmation redis lock failed",
				logx.Field("confirmation_id", confirmationID), logx.Field("err", releaseErr))
		}
	}()
	return operation()
}

func (m *Manager) loadOwned(ctx context.Context, userID uint64, conversationID, confirmationID string) (*aiconfirmations.AiConfirmations, error) {
	if m.model == nil {
		return nil, ErrInvalidConfirmation
	}
	row, err := m.model.FindOneUncached(ctx, confirmationID)
	if errors.Is(err, aiconfirmations.ErrNotFound) {
		return nil, ErrConfirmationNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.UserId != userID || row.ConversationId != conversationID {
		return nil, ErrConfirmationForbidden
	}
	return row, nil
}

// 转换结果为Confirmation对象
func confirmationFromRow(row *aiconfirmations.AiConfirmations) (*domain.Confirmation, error) {
	arguments := make(map[string]any)
	if err := json.Unmarshal([]byte(row.Arguments), &arguments); err != nil {
		return nil, fmt.Errorf("decode confirmation arguments: %w", err)
	}
	confirmation := &domain.Confirmation{
		ID:             row.Id,
		ConversationID: row.ConversationId,
		UserID:         row.UserId,
		ToolName:       row.ToolName,
		Arguments:      arguments,
		Summary:        row.Summary,
		Status:         row.Status,
		RunID:          row.RunId,
		CheckpointID:   row.CheckpointId,
		InterruptID:    row.InterruptId,
		ExpiresAt:      row.ExpiresAt,
	}
	if row.ExecutedAt.Valid {
		executedAt := row.ExecutedAt.Time
		confirmation.ExecutedAt = &executedAt
	}
	return confirmation, nil
}

func confirmationStateError(status string) error {
	switch status {
	case StatusExpired:
		return ErrConfirmationExpired
	case StatusRejected:
		return ErrConfirmationRejected
	default:
		return ErrConfirmationAlreadyProcessed
	}
}

func (m *Manager) now() time.Time {
	return m.clock().Truncate(time.Second)
}

func newConfirmationID() string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return "confirm_" + id.String()
}

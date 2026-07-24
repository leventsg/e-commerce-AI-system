package contextmanager

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const inferredMemoryMinConfidence = 0.85

var (
	ErrInvalidMemoryCommand = errors.New("invalid memory command")
	ErrRejectedMemory       = errors.New("memory candidate rejected")
	// 敏感字段
	sensitiveMemoryPattern = regexp.MustCompile(`(?i)\b(user_id|token|session_id|auth|password|passwd|secret|api[_-]?key|cookie|支付密码|验证码|身份证|银行卡)\b`)
	// 完整地址
	fullAddressMemoryPattern = regexp.MustCompile(`(?i)(完整地址|详细地址|收货地址).{0,40}(省|市|区|县|路|街|号)`)
)

type MemoryPersistence interface {
	FindByKey(ctx context.Context, userID uint64, key string) (*domain.UserMemory, error)
	Upsert(ctx context.Context, memory *domain.UserMemory) (*domain.UserMemory, error)
	ExpireDue(ctx context.Context, userID uint64, now time.Time) (int, error)
}

type MemoryPolicy struct {
	store MemoryPersistence
	now   func() time.Time
}

type MemoryPolicyOption func(*MemoryPolicy)

func WithMemoryPolicyClock(clock func() time.Time) MemoryPolicyOption {
	return func(p *MemoryPolicy) {
		if clock != nil {
			p.now = clock
		}
	}
}

// 显式
type MemoryCommand struct {
	UserID          uint64
	MemoryKey       string
	MemoryType      string
	Content         string
	SourceMessageID string
	TTL             time.Duration
}

// 推断
type MemoryCandidate struct {
	UserID          uint64
	MemoryKey       string
	MemoryType      string
	Content         string
	Confidence      float64
	SourceMessageID string
	TTL             time.Duration
}

func NewMemoryPolicy(store MemoryPersistence, opts ...MemoryPolicyOption) *MemoryPolicy {
	p := &MemoryPolicy{store: store, now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// SaveExplicit 保存显式记忆，用户明确指定的记忆内容，通常是用户主动输入的指令或偏好
func (p *MemoryPolicy) SaveExplicit(ctx context.Context, cmd MemoryCommand) (*domain.UserMemory, error) {
	if p == nil || p.store == nil || !validMemoryBase(cmd.UserID, cmd.MemoryKey, cmd.MemoryType, cmd.Content) {
		return nil, ErrInvalidMemoryCommand
	}
	now := p.now()
	existing, err := p.store.FindByKey(ctx, cmd.UserID, cmd.MemoryKey)
	if err != nil {
		return nil, err
	}
	memory := &domain.UserMemory{}
	if existing != nil {
		*memory = *existing
	} else {
		memory.Id = newMemoryID()
	}
	memory.UserID = cmd.UserID
	memory.Key = cmd.MemoryKey
	memory.Type = cmd.MemoryType
	memory.Content = cmd.Content
	memory.Confidence = 1
	memory.Source = domain.MemorySourceExplicit
	memory.SourceMessageID = cmd.SourceMessageID
	memory.Status = domain.MemoryStatusActive
	memory.ExpiresAt = expiresAt(now, cmd.TTL)
	memory.LastConfirmedAt = &now
	return p.store.Upsert(ctx, memory)
}

// SaveInferred 保存推断记忆，AI通过对话推断出的用户偏好
func (p *MemoryPolicy) SaveInferred(ctx context.Context, candidate MemoryCandidate) (*domain.UserMemory, error) {
	// 检查推断记忆是否符合要求
	if p == nil || p.store == nil ||
		!validMemoryBase(candidate.UserID, candidate.MemoryKey, candidate.MemoryType, candidate.Content) ||
		candidate.Confidence < inferredMemoryMinConfidence ||
		candidate.SourceMessageID == "" ||
		candidate.TTL <= 0 ||
		containsSensitiveMemory(candidate.Content) {
		return nil, ErrRejectedMemory
	}
	now := p.now()
	existing, err := p.store.FindByKey(ctx, candidate.UserID, candidate.MemoryKey)
	if err != nil {
		return nil, err
	}
	memory := &domain.UserMemory{}
	if existing != nil {
		*memory = *existing
	} else {
		memory.Id = newMemoryID()
	}
	memory.UserID = candidate.UserID
	memory.Key = candidate.MemoryKey
	memory.Type = candidate.MemoryType
	memory.Content = candidate.Content
	memory.Confidence = candidate.Confidence
	memory.Source = domain.MemorySourceInferred
	memory.SourceMessageID = candidate.SourceMessageID
	memory.Status = domain.MemoryStatusActive
	memory.ExpiresAt = expiresAt(now, candidate.TTL)
	return p.store.Upsert(ctx, memory)
}

func (p *MemoryPolicy) Delete(ctx context.Context, userID uint64, key string) error {
	if p == nil || p.store == nil || userID == 0 || key == "" {
		return ErrInvalidMemoryCommand
	}
	memory, err := p.store.FindByKey(ctx, userID, key)
	if err != nil || memory == nil {
		return err
	}
	// 软删除
	memory.Status = domain.MemoryStatusDeleted
	_, err = p.store.Upsert(ctx, memory)
	return err
}

// ExpireDue 过期处理，清理过期的记忆
func (p *MemoryPolicy) ExpireDue(ctx context.Context, userID uint64) (int, error) {
	if p == nil || p.store == nil || userID == 0 {
		return 0, ErrInvalidMemoryCommand
	}
	return p.store.ExpireDue(ctx, userID, p.now())
}

func validMemoryBase(userID uint64, key, typ, content string) bool {
	if userID == 0 || key == "" || content == "" {
		return false
	}
	switch typ {
	case domain.MemoryTypeInstruction, domain.MemoryTypePreference, domain.MemoryTypePrice, domain.MemoryTypeProfileFact:
		return true
	default:
		return false
	}
}

// containsSensitiveMemory 检查内容中是否包含敏感信息
func containsSensitiveMemory(content string) bool {
	return sensitiveMemoryPattern.MatchString(content) || fullAddressMemoryPattern.MatchString(content)
}

func expiresAt(now time.Time, ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	expires := now.Add(ttl)
	return &expires
}

func newMemoryID() string {
	return "mem_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

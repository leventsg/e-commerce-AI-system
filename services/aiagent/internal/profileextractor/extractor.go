package profileextractor

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	UpdateTypeExplicitPreference = "explicit_preference"
	UpdateTypeStablePattern      = "stable_pattern"
	UpdateTypeCorrection         = "correction"
	UpdateTypeDeleteOrForget     = "delete_or_forget"
	UpdateTypeNone               = "none"

	stablePatternMinConfidence = 0.85
)

var (
	ErrInvalidEvent      = errors.New("invalid profile update event")
	ErrRejectedCandidate = errors.New("profile candidate rejected")

	sensitiveProfilePattern = regexp.MustCompile(`(?i)(user_id|token|session_id|auth|password|passwd|secret|api[_-]?key|cookie|支付密码|验证码|身份证|银行卡|完整地址|详细地址|收货地址)`)
)

// 更新事件
type UpdateEvent struct {
	EventID        string    `json:"event_id"`
	UserID         uint64    `json:"user_id"`
	ConversationID string    `json:"conversation_id"`
	MessageIDs     []string  `json:"message_ids"`
	CreatedAt      time.Time `json:"created_at"`
}

// AI 模型输出的候选更新
type Candidate struct {
	ShouldUpdate       bool            `json:"should_update"`
	UpdateType         string          `json:"update_type"`
	ProfilePatch       json.RawMessage `json:"profile_patch"`        //配置补丁
	EvidenceMessageIDs []string        `json:"evidence_message_ids"` //证据消息id
	Confidence         float64         `json:"confidence"`
	Reason             string          `json:"reason"`
}

// 解析ai模型输出的候选更新结果
func ParseCandidate(output string) (Candidate, error) {
	if output == "" || !strings.HasPrefix(strings.TrimLeft(output, " \t\r\n"), "{") {
		return Candidate{}, ErrRejectedCandidate
	}
	var candidate Candidate
	if err := json.Unmarshal([]byte(output), &candidate); err != nil {
		return Candidate{}, err
	}
	if len(candidate.ProfilePatch) > 0 && !json.Valid(candidate.ProfilePatch) {
		return Candidate{}, ErrRejectedCandidate
	}
	return candidate, nil
}

type ExtractRequest struct {
	Event    UpdateEvent
	Messages []*aimessages.AiMessages
	Profile  *domain.UserProfile
	Memories []domain.UserMemory
}

type MessageStore interface {
	FindMessagesByIDs(ctx context.Context, userID uint64, conversationID string, messageIDs []string) ([]*aimessages.AiMessages, error)
}

type ProfileStore interface {
	LoadActive(ctx context.Context, userID uint64) (*domain.UserProfile, error)
	Upsert(ctx context.Context, profile *domain.UserProfile, userID uint64) (*domain.UserProfile, error)
}

type MemoryStore interface {
	ListActive(ctx context.Context, userID uint64, limit int) ([]domain.UserMemory, error)
}

type Model interface {
	// Extract 从用户消息和上下文中提取用户画像更新候选
	Extract(ctx context.Context, req ExtractRequest) (Candidate, error)
}

type Extractor struct {
	messages MessageStore
	profiles ProfileStore
	memories MemoryStore
	model    Model
}

func NewExtractor(messages MessageStore, profiles ProfileStore, memories MemoryStore, model Model) *Extractor {
	return &Extractor{messages: messages, profiles: profiles, memories: memories, model: model}
}

func (e *Extractor) Handle(ctx context.Context, event UpdateEvent) error {
	// 参数校验
	if e == nil || e.messages == nil || e.profiles == nil || e.model == nil ||
		event.EventID == "" || event.UserID == 0 || event.ConversationID == "" || len(event.MessageIDs) == 0 {
		return ErrInvalidEvent
	}
	// 加载消息
	messages, err := e.messages.FindMessagesByIDs(ctx, event.UserID, event.ConversationID, event.MessageIDs)
	if err != nil {
		return err
	}
	// 获取用户画像
	profile, err := e.profiles.LoadActive(ctx, event.UserID)
	if err != nil {
		return err
	}
	var memories []domain.UserMemory
	// 获取用户近期记忆
	if e.memories != nil {
		memories, _ = e.memories.ListActive(ctx, event.UserID, 12)
	}
	candidate, err := e.model.Extract(ctx, ExtractRequest{
		Event: event, Messages: messages, Profile: profile, Memories: memories,
	})
	if err != nil {
		return err
	}
	next, err := buildNextProfile(profile, event, messages, candidate)
	if err != nil || next == nil {
		return err
	}
	_, err = e.profiles.Upsert(ctx, next, event.UserID)
	return err
}

func buildNextProfile(current *domain.UserProfile, event UpdateEvent, messages []*aimessages.AiMessages, candidate Candidate) (*domain.UserProfile, error) {
	// 如果不需要更新，直接返回 nil
	if !candidate.ShouldUpdate || candidate.UpdateType == UpdateTypeNone {
		return nil, nil
	}
	// 如果候选不合法，拒绝更新
	if !validCandidate(event, messages, candidate) {
		return nil, ErrRejectedCandidate
	}

	// 解析参数
	base := map[string]any{}
	if current != nil && len(current.ProfileJSON) > 0 {
		if err := json.Unmarshal(current.ProfileJSON, &base); err != nil {
			return nil, ErrRejectedCandidate
		}
	}
	patch := map[string]any{}
	if err := json.Unmarshal(candidate.ProfilePatch, &patch); err != nil {
		return nil, ErrRejectedCandidate
	}
	merged := mergeProfileMap(base, patch)
	raw, err := json.Marshal(merged)
	if err != nil || !json.Valid(raw) {
		return nil, ErrRejectedCandidate
	}
	return &domain.UserProfile{
		ProfileJSON: raw,
		Version:     nextProfileVersion(current),
		LastEventID: event.EventID,
		UpdatedAt:   event.CreatedAt,
	}, nil
}

// validCandidate 验证候选是否有效
func validCandidate(event UpdateEvent, messages []*aimessages.AiMessages, candidate Candidate) bool {
	switch candidate.UpdateType {
	case UpdateTypeExplicitPreference, UpdateTypeCorrection, UpdateTypeDeleteOrForget:
	case UpdateTypeStablePattern:
		// 对于稳定模式，要求置信度达到阈值0.85
		if candidate.Confidence < stablePatternMinConfidence {
			return false
		}
	default:
		return false
	}
	if len(candidate.ProfilePatch) == 0 || !json.Valid(candidate.ProfilePatch) || len(candidate.EvidenceMessageIDs) == 0 {
		return false
	}
	allowedEvidence := make(map[string]bool, len(messages))
	for _, message := range messages {
		if message != nil && message.UserId == event.UserID && message.ConversationId == event.ConversationID {
			allowedEvidence[message.Id] = true
		}
	}
	// 如果候选证据消息中有不属于当前用户会话的消息，则拒绝
	for _, id := range candidate.EvidenceMessageIDs {
		if !allowedEvidence[id] {
			return false
		}
	}
	return !containsSensitiveProfileJSON(candidate.ProfilePatch)
}

// 增量更新
func mergeProfileMap(base map[string]any, patch map[string]any) map[string]any {
	for key, patchValue := range patch {
		if patchValue == nil {
			delete(base, key) // nil 值表示删除
			continue
		}
		patchMap, patchIsMap := patchValue.(map[string]any)
		baseMap, baseIsMap := base[key].(map[string]any)
		if patchIsMap && baseIsMap {
			base[key] = mergeProfileMap(baseMap, patchMap)
			continue
		}
		base[key] = patchValue
	}
	return base
}

// 检查 JSON 中是否包含敏感信息
func containsSensitiveProfileJSON(raw json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return true
	}
	return containsSensitiveProfileValue(value)
}

func containsSensitiveProfileValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			if sensitiveProfilePattern.MatchString(key) || containsSensitiveProfileValue(inner) {
				return true
			}
		}
	case []any:
		for _, inner := range typed {
			if containsSensitiveProfileValue(inner) {
				return true
			}
		}
	case string:
		return sensitiveProfilePattern.MatchString(typed)
	}
	return false
}

func nextProfileVersion(current *domain.UserProfile) uint64 {
	if current == nil || current.Version == 0 {
		return 1
	}
	return current.Version + 1
}

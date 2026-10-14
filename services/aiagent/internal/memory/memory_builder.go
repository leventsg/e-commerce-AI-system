package memory

import (
	"context"
	"sort"
	"strings"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// MemoryBuilder 是记忆数据源的纯取数模块：只负责从存储检索近期对话、会话摘要、
// 用户画像与长期事件，不计算 token、不触发摘要压缩。token 预算与压缩由
// ContextBuilder 负责。
type MemoryBuilder struct {
	messages  MessageStore
	summaries SummaryStore
	profiles  UserProfileStore
	events    MemoryEventStore
}

func NewMemoryBuilder(messages MessageStore, summaries SummaryStore, profiles UserProfileStore, events MemoryEventStore) *MemoryBuilder {
	return &MemoryBuilder{
		messages:  messages,
		summaries: summaries,
		profiles:  profiles,
		events:    events,
	}
}

// LoadSummary 返回最新会话摘要（可能为 nil）。
func (b *MemoryBuilder) LoadSummary(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error) {
	if b.summaries == nil {
		return nil, nil
	}
	return b.summaries.FindLatest(ctx, userID, conversationID)
}

// LoadHistory 返回近期对话的原始行（已过滤工具消息、当前消息与摘要覆盖部分，并
// 按时间排序），未做 token 滑动窗口裁剪。
func (b *MemoryBuilder) LoadHistory(ctx context.Context, userID uint64, conversationID string, currentMessageID string, summary *domain.ConversationSummary) ([]*aimessages.AiMessages, error) {
	if b.messages == nil {
		return nil, nil
	}
	rows, err := b.messages.FindRecent(ctx, userID, conversationID, defaultHistoryQueryLimit)
	if err != nil {
		return nil, err
	}
	return filterAndSortHistory(rows, currentMessageID, summary), nil
}

// LoadProfile 返回用户画像（可能为 nil）。
func (b *MemoryBuilder) LoadProfile(ctx context.Context, userID uint64) (*domain.UserProfile, error) {
	if b.profiles == nil {
		return nil, nil
	}
	return b.profiles.LoadActive(ctx, userID)
}

// LoadEvents 返回近期长期事件与相关长期事件（相关已对近期去重）。query 与
// agentName 由调用方从当前输入提取，用于相关事件的语义检索。
func (b *MemoryBuilder) LoadEvents(ctx context.Context, userID uint64, query, agentName string) (recent, relevant []Event, err error) {
	if b.events == nil {
		return nil, nil, nil
	}
	recent, err = b.events.ListRecentUserMemoryEvents(ctx, userID, defaultRelevantMemoryEventLimit)
	if err != nil {
		return nil, nil, err
	}
	relevant, err = b.retrieveRelevantEvents(ctx, userID, query, agentName)
	if err != nil {
		return recent, nil, err
	}
	relevant = dedupeEvents(relevant, recent)
	return recent, relevant, nil
}

func (b *MemoryBuilder) retrieveRelevantEvents(ctx context.Context, userID uint64, query, agentName string) ([]Event, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	return b.events.SearchUserMemoryEvents(ctx, UserMemoryEventQuery{
		UserID:   userID,
		Match:    query,
		Keywords: recallKeywords(query, agentName),
		Limit:    defaultRelevantMemoryEventLimit,
	})
}

// filterAndSortHistory 过滤工具消息、当前消息与摘要覆盖部分，并按时间升序排序。
// 不做 token 计算；滑动窗口裁剪由 windowAndRedactHistory 完成。
func filterAndSortHistory(rows []*aimessages.AiMessages, currentMessageID string, summary *domain.ConversationSummary) []*aimessages.AiMessages {
	filtered := make([]*aimessages.AiMessages, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.MsgId == currentMessageID || strings.TrimSpace(row.Content) == "" {
			continue
		}
		if row.Role != domain.ContextRoleUser && row.Role != domain.ContextRoleAssistant {
			continue
		}
		if coveredBySummary(row, summary) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].MsgId < filtered[j].MsgId
		}
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	return filtered
}

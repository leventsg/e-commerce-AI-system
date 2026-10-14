package memory

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// MemoryProvider abstracts online model-context retrieval.
// The customer-service implementation reads conversation, summary, tool,
// user profile, and user memory event stores.
type MemoryProvider interface {
	Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResult, error)
	Memorize(ctx context.Context, req *MemorizeRequest) error
	Close() error
}

// Compressor 在对话流超预算时同步压缩最旧的对话流（用户/助手消息）为摘要并推进水位。
// 压缩是惰性的：仅当未压缩消息累计 token 达到阈值时才真正压缩（调摘要模型 + 写库）。
// 用于每次模型调用前的动态压缩，保护长期事件/用户画像/system prompt/RAG 等结构化内容不被压缩。
// 返回本次压缩的消息 ID（nil 表示未发生压缩）。
type Compressor interface {
	CompressIfNeeded(ctx context.Context, userID uint64, conversationID string) ([]string, error)
}

// CompressCallback 在压缩发生后被调用，用于发布被压缩的消息 ID（如给画像/记忆提取消费）。
// 传入的 ctx 可能已取消，实现应自行处理（如 context.WithoutCancel）。
type CompressCallback func(ctx context.Context, userID uint64, conversationID string, messageIDs []string)

type RetrieveRequest struct {
	UserID           uint64
	ConversationID   string
	AgentName        string
	RunID            string
	CurrentMessageID string
	ClientMessageID  string
	Messages         []domain.ContextMessage
	RAGContext       []domain.ContextMessage
	Limit            int
	// SkipStable 为 true 时跳过稳定前缀（用户画像 + 长期事件）的检索，返回的
	// SystemMessages 为空。用于同一 turn 内后续模型调用：稳定前缀已缓存在 ctx，
	// 每次只刷新易变后缀。
	SkipStable bool
}

type RetrieveResult struct {
	SystemMessages  []domain.ContextMessage
	HistoryMessages []domain.ContextMessage
	ContextMessages []domain.ContextMessage
	Metadata        map[string]any
}

type MemorizeRequest struct {
	UserID          uint64
	ConversationID  string
	RunID           string
	ClientMessageID string
	Messages        []domain.ContextMessage
	MessageIDs      []string
}

type Event struct {
	UserID    uint64
	Type      string
	EventDate string
	Summary   string
	Keywords  []string
}

type UserMemoryEventQuery struct {
	UserID   uint64
	Keywords []string
	Match    string
	Type     string
	Since    string
	Until    string
	Limit    int
}

type ToolCallMemory struct {
	ToolName          string
	ToolCallID        string
	ToolCallArguments string
}

// UserMemoryEventSearcher provides optional long-term memory event lookup.
// Tools must set UserID from trusted session metadata rather than model input.
type UserMemoryEventSearcher interface {
	SearchUserMemoryEvents(ctx context.Context, query UserMemoryEventQuery) ([]Event, error)
}

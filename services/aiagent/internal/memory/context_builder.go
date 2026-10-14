package memory

import (
	"context"
	"strings"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// ContextBuilder 是上下文编排模块：负责摘要压缩、token 预算（滑动窗口 + 各层
// cap）以及「稳定前缀 / 易变后缀」分层组装。它调用 MemoryBuilder 取数，接收
// RAG 结果作为易变输入。
type ContextBuilder struct {
	memory       *MemoryBuilder
	compressor   Compressor
	tools        ToolContextStore
	tasks        TaskStateStore
	budget       domain.TokenBudget
	estimator    domain.TokenEstimator
	now          func() time.Time
	onCompressed CompressCallback
}

type ContextBuilderConfig struct {
	Memory       *MemoryBuilder
	Compressor   Compressor
	Tools        ToolContextStore
	Tasks        TaskStateStore
	Budget       domain.TokenBudget
	OnCompressed CompressCallback
	Now          func() time.Time
}

func NewContextBuilder(cfg ContextBuilderConfig) *ContextBuilder {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &ContextBuilder{
		memory:       cfg.Memory,
		compressor:   cfg.Compressor,
		tools:        cfg.Tools,
		tasks:        cfg.Tasks,
		budget:       cfg.Budget.Normalized(),
		estimator:    domain.HybridEstimator{},
		now:          now,
		onCompressed: cfg.OnCompressed,
	}
}

// VolatileResult 是易变后缀的组装结果，每次模型调用前刷新。
type VolatileResult struct {
	HistoryMessages []domain.ContextMessage
	ContextMessages []domain.ContextMessage
	Metadata        map[string]any
}

// PrepareStable 返回稳定前缀（用户画像 + 长期事件），一次 turn 内只应调用一次。
// 注入角色为带标签的 user 消息；system prompt 由 agent 构建层注入，不在这里。
func (b *ContextBuilder) PrepareStable(ctx context.Context, req *RetrieveRequest) []domain.ContextMessage {
	blocks := make([]domain.ContextMessage, 0, 4)
	if req == nil || req.UserID == 0 || b.memory == nil {
		return blocks
	}
	query := eventRecallQuery(req)
	recent, relevant, err := b.memory.LoadEvents(ctx, req.UserID, query, req.AgentName)
	if err == nil {
		if len(recent) > 0 {
			if msg, ok := jsonBlock("user_memory_recent_events", recent); ok {
				blocks = append(blocks, msg)
			}
		}
		if len(relevant) > 0 {
			if msg, ok := jsonBlock("user_memory_relevant_events", relevant); ok {
				blocks = append(blocks, msg)
			}
		}
	}
	if profile, err := b.memory.LoadProfile(ctx, req.UserID); err == nil && profile != nil {
		if msg, ok := jsonBlock("user_profile", profile.ProfileJSON); ok {
			blocks = append(blocks, msg)
		}
	}
	return blocks
}

// RefreshVolatile 压缩 + 刷新易变后缀（摘要 + 历史 + 任务状态 + 工具上下文 +
// 当前时间 + RAG）。每次模型调用前调用；压缩是惰性的，未超阈值不真正压缩。
func (b *ContextBuilder) RefreshVolatile(ctx context.Context, req *RetrieveRequest) (*VolatileResult, error) {
	result := &VolatileResult{Metadata: map[string]any{}}
	if req == nil || req.UserID == 0 || req.ConversationID == "" {
		return result, nil
	}

	// 1. 动态压缩（惰性）：每次模型调用前判断对话流是否超预算，超则同步压缩。
	if b.compressor != nil {
		compressed, err := b.compressor.CompressIfNeeded(ctx, req.UserID, req.ConversationID)
		if err != nil {
			result.Metadata["context_compress_degraded"] = err.Error()
		} else if len(compressed) > 0 {
			result.Metadata["context_compressed"] = true
			if b.onCompressed != nil {
				go b.onCompressed(context.WithoutCancel(ctx), req.UserID, req.ConversationID, compressed)
			}
		}
	}

	// 2. 摘要 + 历史（摘要压缩后重新加载）。
	var summary *domain.ConversationSummary
	if b.memory != nil {
		if loaded, err := b.memory.LoadSummary(ctx, req.UserID, req.ConversationID); err == nil {
			summary = loaded
			if summary != nil {
				result.Metadata["summary_covered_message_id"] = summary.CoveredUntilMessageID
			}
		} else {
			result.Metadata["summary_degraded"] = err.Error()
		}
		if rows, err := b.memory.LoadHistory(ctx, req.UserID, req.ConversationID, req.CurrentMessageID, summary); err == nil {
			result.HistoryMessages = windowAndRedactHistory(rows, b.budget, b.estimator)
			if len(result.HistoryMessages) > 0 {
				result.Metadata["recent_message_count"] = len(result.HistoryMessages)
			}
		} else {
			result.Metadata["history_degraded"] = err.Error()
		}
	}

	// 3. 易变上下文块：当前时间 → 摘要 → 工具上下文 → 任务状态 → RAG。
	blocks := make([]domain.ContextMessage, 0, 8)
	blocks = append(blocks, domain.ContextMessage{Role: domain.ContextRoleUser, Content: formatBlock("current_time", b.now().Format(time.RFC3339))})
	if summary != nil {
		if msg, ok := jsonBlock("conversation_context", summary); ok {
			blocks = append(blocks, msg)
		}
	}
	b.appendToolContext(ctx, req, &blocks, result.Metadata)
	b.appendTaskState(ctx, req, &blocks, result.Metadata)
	for _, message := range req.RAGContext {
		if strings.TrimSpace(message.Content) != "" {
			blocks = append(blocks, message)
		}
	}
	result.ContextMessages = blocks
	return result, nil
}

func (b *ContextBuilder) appendToolContext(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if b.tools == nil {
		return
	}
	latest, err := b.tools.FindLatestToolResult(ctx, req.UserID, req.ConversationID)
	if err == nil && latest != nil {
		if msg, ok := jsonBlock("latest_tool_result", latest); ok {
			msg.Content = b.clampBlock(msg.Content, b.budget.LatestToolResultMaxTokens)
			*blocks = append(*blocks, msg)
			meta["latest_tool_call_id"] = latest.ToolCallId
		}
	} else if err != nil {
		meta["tool_result_degraded"] = err.Error()
	}
	recent, err := b.tools.FindRecentToolCall(ctx, req.UserID, req.ConversationID, defaultRecentToolRefLimit)
	if err != nil {
		meta["recent_tool_calls_degraded"] = err.Error()
		return
	}
	historical := make([]*ToolCallMemory, 0, len(recent))
	for _, call := range recent {
		if call == nil || (latest != nil && call.ToolCallId == latest.ToolCallId) {
			continue
		}
		historical = append(historical, &ToolCallMemory{
			ToolName:          call.ToolName,
			ToolCallID:        call.ToolCallId,
			ToolCallArguments: call.Arguments,
		})
	}
	if len(historical) == 0 {
		return
	}
	if msg, ok := jsonBlock("recent_tool_calls", historical); ok {
		msg.Content = b.clampBlock(msg.Content, b.budget.ToolHistoryMaxTokens)
		*blocks = append(*blocks, msg)
		meta["recent_tool_call_count"] = len(historical)
	}
}

func (b *ContextBuilder) appendTaskState(ctx context.Context, req *RetrieveRequest, blocks *[]domain.ContextMessage, meta map[string]any) {
	if b.tasks == nil || req.RunID == "" {
		return
	}
	state, err := b.tasks.FindActive(ctx, req.UserID, req.ConversationID, req.RunID)
	if err != nil {
		meta["task_state_degraded"] = err.Error()
		return
	}
	if state == nil {
		return
	}
	if msg, ok := jsonBlock("task_state", state); ok {
		*blocks = append(*blocks, msg)
	}
}

// clampBlock 将上下文块截断到 maxTokens 以内，用于可压缩对话流层的预算约束。
func (b *ContextBuilder) clampBlock(content string, maxTokens int) string {
	if maxTokens <= 0 {
		return content
	}
	return domain.TruncateToTokens(content, maxTokens, b.estimator)
}

// windowAndRedactHistory 对已过滤排序的历史做 token 滑动窗口（从新到旧累计，
// 触顶即停，预算耗尽保底保留 MinRecentMessages 条），并脱敏。
func windowAndRedactHistory(filtered []*aimessages.AiMessages, budget domain.TokenBudget, estimator domain.TokenEstimator) []domain.ContextMessage {
	remaining := budget.RecentWindowTokens
	minRecent := budget.MinRecentMessages
	kept := make([]*aimessages.AiMessages, 0, len(filtered))
	for i := len(filtered) - 1; i >= 0; i-- {
		cost := estimator.Tokens(filtered[i].Content)
		if len(kept) >= minRecent && remaining < cost {
			break
		}
		kept = append([]*aimessages.AiMessages{filtered[i]}, kept...)
		remaining -= cost
	}
	result := make([]domain.ContextMessage, 0, len(kept))
	for _, row := range kept {
		result = append(result, domain.ContextMessage{Role: row.Role, Content: redactSensitiveContext(row.Content)})
	}
	return result
}

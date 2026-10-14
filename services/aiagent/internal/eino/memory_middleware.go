package eino

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
	"github.com/zeromicro/go-zero/core/logx"
)

type memoryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	provider  aimemory.MemoryProvider
	agentName string
}

// memoryStablePrefixKey 缓存一次 turn 内不变的稳定前缀（用户画像 + 长期事件），
// 使后续模型调用跳过稳定检索、只刷新易变后缀，从而保护 prompt cache 前缀。
type memoryStablePrefixKey struct{}

func NewMemoryMiddleware(provider aimemory.MemoryProvider) adk.ChatModelAgentMiddleware {
	return NewMemoryMiddlewareForAgent(provider, "")
}

func NewMemoryMiddlewareForAgent(provider aimemory.MemoryProvider, agentName string) adk.ChatModelAgentMiddleware {
	return &memoryMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		provider:                     provider,
		agentName:                    strings.TrimSpace(agentName),
	}
}

// BeforeModelRewriteState：每次模型调用前调用
func (m *memoryMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	// 检查上下文
	if m == nil || m.provider == nil || state == nil {
		return ctx, state, nil
	}
	// 获取会话元信息
	meta, ok := memoryConversationMetadata(ctx)
	if !ok {
		return ctx, state, nil
	}
	// 稳定前缀（用户画像 + 长期事件）在 turn 内只检索一次并缓存到 ctx；后续模型调用
	// 通过 SkipStable 跳过稳定检索，只刷新易变后缀（压缩 + 摘要 + 历史 + 任务 + 工具 + RAG），
	// 使稳定前缀字节在 turn 内不变，从而保护 prompt cache 前缀命中。
	stableMessages, stableCached := m.stableFromCtx(ctx)
	retrieveCtx, endRetrieve := obs.Start(ctx, "context.assemble", map[string]string{"agent": m.agentName, "run_id": meta.RunID})
	result, err := m.provider.Retrieve(retrieveCtx, &aimemory.RetrieveRequest{
		UserID:           meta.UserID,
		ConversationID:   meta.ConversationID,
		AgentName:        m.agentName,
		RunID:            meta.RunID,
		CurrentMessageID: meta.CurrentMessageID,
		ClientMessageID:  meta.ClientMessageID,
		Messages:         schemaMessagesToContext(state.Messages),
		RAGContext:       meta.RAGContext,
		SkipStable:       stableCached,
	})
	if err != nil {
		endRetrieve("failed")
	} else {
		endRetrieve("success")
	}
	logx.Infof("上下文组装结果：%s, userid: %d, conversation_id: %s", result, meta.UserID, meta.ConversationID)
	if err != nil || result == nil {
		return ctx, state, err
	}
	// 首次调用：把稳定前缀转成 schema 消息并缓存到 ctx，供本 turn 后续调用复用。
	if !stableCached {
		converted, err := ConvertContextMessages(result.SystemMessages)
		if err != nil {
			return ctx, state, err
		}
		stableMessages = converted
		ctx = m.withStableInCtx(ctx, stableMessages)
	}
	system, rest := splitFirstSystemMessage(state.Messages)
	runtimeContext := joinRuntimeContext(result.ContextMessages)
	nextRest := appendRuntimeContextToLatestUser(rest, runtimeContext)
	history, err := ConvertContextMessages(result.HistoryMessages)
	if err != nil {
		return ctx, state, err
	}
	enhanced := make([]*schema.Message, 0, len(stableMessages)+1+len(history)+len(nextRest)+1)
	if system != nil {
		enhanced = append(enhanced, system)
	}
	enhanced = append(enhanced, stableMessages...)
	enhanced = append(enhanced, history...)
	enhanced = append(enhanced, nextRest...)
	state.Messages = enhanced
	return ctx, state, nil
}

// stableFromCtx 从 ctx 取出已缓存的稳定前缀；第二个返回值为 false 表示尚未缓存
//（本 turn 首次模型调用）。
func (m *memoryMiddleware) stableFromCtx(ctx context.Context) ([]*schema.Message, bool) {
	value, ok := ctx.Value(memoryStablePrefixKey{}).([]*schema.Message)
	return value, ok
}

// withStableInCtx 把稳定前缀缓存到 ctx，供同一 turn 内后续模型调用复用。
func (m *memoryMiddleware) withStableInCtx(ctx context.Context, messages []*schema.Message) context.Context {
	return context.WithValue(ctx, memoryStablePrefixKey{}, messages)
}

// 从会话值中获取上下文元信息
func memoryConversationMetadata(ctx context.Context) (aimemory.ConversationMetadata, bool) {
	values := adk.GetSessionValues(ctx)
	if meta, ok := aimemory.ConversationMetadataFromMap(values); ok {
		return meta, true
	}
	return aimemory.ConversationMetadataFromContext(ctx)
}

func splitFirstSystemMessage(messages []*schema.Message) (*schema.Message, []*schema.Message) {
	rest := make([]*schema.Message, 0, len(messages))
	var system *schema.Message
	for _, message := range messages {
		if message != nil && system == nil && message.Role == schema.System {
			system = message
			continue
		}
		rest = append(rest, message)
	}
	return system, rest
}

func appendRuntimeContextToLatestUser(messages []*schema.Message, runtimeContext string) []*schema.Message {
	if runtimeContext == "" {
		return messages
	}
	next := append([]*schema.Message(nil), messages...)
	for i := len(next) - 1; i >= 0; i-- {
		if next[i] == nil || next[i].Role != schema.User {
			continue
		}
		updated := cloneSchemaMessage(next[i])
		updated.Content = strings.TrimRight(updated.Content, "\n") + "\n\n-----\n" + runtimeContext
		next[i] = updated
		return next
	}
	next = append(next, schema.UserMessage(runtimeContext))
	return next
}

func joinRuntimeContext(messages []domain.ContextMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(message.Content)
	}
	return builder.String()
}

func schemaMessagesToContext(messages []*schema.Message) []domain.ContextMessage {
	result := make([]domain.ContextMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		result = append(result, domain.ContextMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolName:   message.ToolName,
		})
	}
	return result
}

func cloneSchemaMessage(message *schema.Message) *schema.Message {
	if message == nil {
		return nil
	}
	cloned := *message
	return &cloned
}

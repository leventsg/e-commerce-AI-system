package eino

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	"github.com/zeromicro/go-zero/core/logx"
)

type memoryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	provider aimemory.MemoryProvider
}

type memoryPreparedContextKey struct{}

func NewMemoryMiddleware(provider aimemory.MemoryProvider) adk.ChatModelAgentMiddleware {
	return &memoryMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		provider:                     provider,
	}
}

// BeforeModelRewriteState：每次模型调用前调用
func (m *memoryMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	// 检查上下文
	if m == nil || m.provider == nil || state == nil {
		return ctx, state, nil
	}
	// 去重，防止重复组装上下文
	if prepared, _ := ctx.Value(memoryPreparedContextKey{}).(bool); prepared {
		return ctx, state, nil
	}
	// 获取会话元信息
	meta, ok := memoryConversationMetadata(ctx)
	if !ok {
		return ctx, state, nil
	}
	result, err := m.provider.Retrieve(ctx, &aimemory.RetrieveRequest{
		UserID:           meta.UserID,
		ConversationID:   meta.ConversationID,
		RunID:            meta.RunID,
		CurrentMessageID: meta.CurrentMessageID,
		ClientMessageID:  meta.ClientMessageID,
		Messages:         schemaMessagesToContext(state.Messages),
		RAGContext:       meta.RAGContext,
	})
	if err != nil || result == nil {
		return ctx, state, err
	}
	system, rest := splitFirstSystemMessage(state.Messages)
	runtimeContext := joinRuntimeContext(result.ContextMessages)
	nextRest := appendRuntimeContextToLatestUser(rest, runtimeContext)
	history, err := ConvertContextMessages(result.HistoryMessages)
	if err != nil {
		return ctx, state, err
	}
	systemMessages, err := ConvertContextMessages(result.SystemMessages)
	if err != nil {
		return ctx, state, err
	}
	enhanced := make([]*schema.Message, 0, len(systemMessages)+1+len(history)+len(nextRest)+1)
	if system != nil {
		enhanced = append(enhanced, system)
	}
	enhanced = append(enhanced, systemMessages...)
	enhanced = append(enhanced, history...)
	enhanced = append(enhanced, nextRest...)
	logx.Infow("上下文组装结果：", logx.Field("messages", enhanced), logx.Field("userid", meta.UserID), logx.Field("conversation_id", meta.ConversationID))
	state.Messages = enhanced
	ctx = context.WithValue(ctx, memoryPreparedContextKey{}, true)
	return ctx, state, nil
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
	if strings.TrimSpace(runtimeContext) == "" {
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
		if strings.TrimSpace(message.Content) == "" {
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

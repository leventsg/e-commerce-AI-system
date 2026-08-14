package eino

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	einocallbacks "github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	callbackutils "github.com/cloudwego/eino/utils/callbacks"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

type agentEventCallbackBridge struct {
	req             RunRequest
	approvalManager *aitools.ApprovalManager
	emit            func(context.Context, domain.AgentEvent) error

	mu                   sync.Mutex
	businessExecuted     bool
	emittedAny           bool
	assistantEmitted     bool
	activeToolMessageIDs map[string][]string
	runState             *AgentRunStateMachine
}

func newAgentEventCallbackBridge(req RunRequest, approvalManager *aitools.ApprovalManager, emit func(context.Context, domain.AgentEvent) error) *agentEventCallbackBridge {
	runState := newAgentRunStateMachine(agentRunConfig{RunID: req.MessageID, ConversationID: req.ConversationID})
	_ = runState.Start()
	return &agentEventCallbackBridge{
		req:                  req,
		approvalManager:      approvalManager,
		emit:                 emit,
		activeToolMessageIDs: make(map[string][]string),
		runState:             runState,
	}
}

func (b *agentEventCallbackBridge) modelHandler() einocallbacks.Handler {
	return callbackutils.NewHandlerHelper().
		ChatModel(&callbackutils.ModelCallbackHandler{
			OnEnd:                 b.onModelEnd,
			OnEndWithStreamOutput: b.onModelEndWithStreamOutput,
			OnError:               b.onModelError,
		}).Handler()
}

func (b *agentEventCallbackBridge) toolHandler() einocallbacks.Handler {
	return callbackutils.NewHandlerHelper().
		Tool(&callbackutils.ToolCallbackHandler{
			OnStart: b.onToolStart,
			OnEnd:   b.onToolEnd,
			OnError: b.onToolError,
		}).Handler()
}

func (b *agentEventCallbackBridge) onModelEnd(ctx context.Context, info *einocallbacks.RunInfo, output *model.CallbackOutput) context.Context {
	if !b.shouldExposeModel(info) || output == nil || output.Message == nil {
		return ctx
	}
	if reasoning := reasoningContent(output.Message); reasoning != "" {
		_ = b.sendThinkingDelta(ctx, reasoning)
	}
	content := output.Message.Content
	if content == "" {
		return ctx
	}
	if len(output.Message.ToolCalls) > 0 {
		_ = b.sendThinkingDelta(ctx, content)
		return ctx
	}
	return ctx
}

func (b *agentEventCallbackBridge) onModelEndWithStreamOutput(ctx context.Context, info *einocallbacks.RunInfo, output *schema.StreamReader[*model.CallbackOutput]) context.Context {
	if output == nil {
		return ctx
	}
	defer output.Close()
	if !b.shouldExposeModel(info) {
		for {
			if _, err := output.Recv(); errors.Is(err, io.EOF) || err != nil {
				return ctx
			}
		}
	}
	for {
		chunk, err := output.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = b.send(ctx, domain.AgentEvent{
				Type:           domain.EventError,
				ConversationID: b.req.ConversationID,
				MessageID:      newAgentMessageID(),
				Content:        fmt.Sprintf("模型流式输出失败：%v", err),
				Status:         "failed",
				Done:           true,
			})
			return ctx
		}
		if chunk == nil || chunk.Message == nil {
			continue
		}
		// 这个思考过程是全英文的
		if reasoning := reasoningContent(chunk.Message); reasoning != "" {
			_ = b.sendThinkingDelta(ctx, reasoning)
		}
		_ = b.sendAssistantDelta(ctx, chunk.Message.Content)
		// if chunk.Message.Content != "" {
		// 	// // 工具调用，作为思考内容发送
		// 	// if len(chunk.Message.ToolCalls) > 0 {
		// 	// 	_ = b.sendThinkingDelta(ctx, chunk.Message.Content)
		// 	// 	continue
		// 	// }
		// 	// 最后assistant输出chunk
		// 	_ = b.sendAssistantDelta(ctx, chunk.Message.Content)
		// }
	}
	return ctx
}

func (b *agentEventCallbackBridge) onModelError(ctx context.Context, info *einocallbacks.RunInfo, err error) context.Context {
	if !b.shouldExposeModel(info) || err == nil {
		return ctx
	}
	_ = b.send(ctx, domain.AgentEvent{
		Type:           domain.EventError,
		ConversationID: b.req.ConversationID,
		MessageID:      newAgentMessageID(),
		Content:        fmt.Sprintf("模型调用失败：%v", err),
		Status:         "failed",
		Done:           true,
	})
	return ctx
}

func (b *agentEventCallbackBridge) onToolStart(ctx context.Context, info *einocallbacks.RunInfo, input *einotool.CallbackInput) context.Context {
	toolName := info.Name
	if toolName == "" || isAgentToolName(toolName) {
		return ctx
	}
	dataJSON := ""
	if input != nil {
		dataJSON = input.ArgumentsInJSON
	}
	messageID := b.startToolMessageID(toolName)
	event, err := b.runState.OnToolProgress(messageID, toolName, toolProgressContent(toolName), ensureJSONObject(dataJSON))
	if err != nil {
		return ctx
	}
	_ = b.send(ctx, event)
	return ctx
}

func (b *agentEventCallbackBridge) onToolEnd(ctx context.Context, info *einocallbacks.RunInfo, output *einotool.CallbackOutput) context.Context {
	toolName := info.Name
	if toolName == "" || isAgentToolName(toolName) {
		return ctx
	}
	response := ""
	if output != nil {
		response = output.Response
		if strings.TrimSpace(response) == "" && output.ToolOutput != nil {
			for _, part := range output.ToolOutput.Parts {
				if part.Type == schema.ToolPartTypeText && strings.TrimSpace(part.Text) != "" {
					response = part.Text
					break
				}
			}
		}
	}
	businessExecuted := isBusinessWriteTool(toolName) || (b.approvalManager != nil && b.approvalManager.RequiresConfirmation(toolName))
	event, stateErr := b.runState.OnToolResult(b.finishToolMessageID(toolName), toolName, "success", wrappedToolSummary(toolName, response), ensureJSONObject(response), businessExecuted)
	if stateErr != nil {
		return ctx
	}
	if event.BusinessExecuted {
		b.mu.Lock()
		b.businessExecuted = true
		b.mu.Unlock()
	}
	_ = b.send(ctx, event)
	return ctx
}

func (b *agentEventCallbackBridge) onToolError(ctx context.Context, info *einocallbacks.RunInfo, err error) context.Context {
	toolName := info.Name
	if toolName == "" || isAgentToolName(toolName) || err == nil {
		return ctx
	}
	event, stateErr := b.runState.OnToolResult(b.finishToolMessageID(toolName), toolName, "failed", "工具调用未完成，请稍后重试。", fmt.Sprintf(`{"error":%q}`, err.Error()), false)
	if stateErr != nil {
		return ctx
	}
	_ = b.send(ctx, event)
	return ctx
}

func (b *agentEventCallbackBridge) shouldExposeModel(info *einocallbacks.RunInfo) bool {
	name := ""
	if info != nil {
		name = strings.TrimSpace(info.Name)
	}
	return name == "" || name == supervisorAgentName || !isKnownAgentName(name)
}

func (b *agentEventCallbackBridge) hasBusinessExecuted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.businessExecuted
}

func (b *agentEventCallbackBridge) hasAnyEvent() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.emittedAny
}

func (b *agentEventCallbackBridge) hasAssistantEvent() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.assistantEmitted
}

func (b *agentEventCallbackBridge) sendThinkingDelta(ctx context.Context, text string) error {
	event, err := b.runState.OnThinkingDelta(text)
	if err != nil || strings.TrimSpace(event.Content) == "" {
		return nil
	}
	return b.send(ctx, event)
}

func (b *agentEventCallbackBridge) sendAssistantDelta(ctx context.Context, text string) error {
	event, err := b.runState.OnModelDelta(text)
	if err != nil || event.Content == "" {
		return nil
	}
	return b.send(ctx, event)
}

// 更新状态机，规范化中断事件
func (b *agentEventCallbackBridge) enterAwaitingConfirmation(event domain.AgentEvent) (domain.AgentEvent, bool) {
	if b.runState.State() == agentRunStateRunning {
		if _, err := b.runState.OnToolProgress(newAgentMessageID(), event.Tool, "", "{}"); err != nil {
			return event, false
		}
	}
	normalized, err := b.runState.OnConfirmationRequired(event.ConfirmationID, event.Tool, event.Summary, event.ExpiresAt)
	if err != nil {
		return event, false
	}
	normalized.MessageID = event.MessageID
	normalized.Content = event.Content
	normalized.Status = event.Status
	normalized.DataJSON = event.DataJSON
	normalized.Action = event.Action
	return normalized, true
}

func (b *agentEventCallbackBridge) startToolMessageID(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	messageID := newAgentMessageID()
	b.mu.Lock()
	b.activeToolMessageIDs[toolName] = append(b.activeToolMessageIDs[toolName], messageID)
	b.mu.Unlock()
	return messageID
}

func (b *agentEventCallbackBridge) finishToolMessageID(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := b.activeToolMessageIDs[toolName]
	if len(ids) == 0 {
		return newAgentMessageID()
	}
	messageID := ids[0]
	if len(ids) == 1 {
		delete(b.activeToolMessageIDs, toolName)
	} else {
		b.activeToolMessageIDs[toolName] = ids[1:]
	}
	return messageID
}

func (b *agentEventCallbackBridge) send(ctx context.Context, event domain.AgentEvent) error {
	b.mu.Lock()
	b.emittedAny = true
	if event.Type == domain.EventAssistantMessage || event.Type == domain.EventAssistantDelta {
		b.assistantEmitted = true
	}
	b.mu.Unlock()
	if b.emit == nil {
		return nil
	}
	return b.emit(ctx, event)
}

func isKnownAgentName(name string) bool {
	if name == supervisorAgentName {
		return true
	}
	for _, spec := range supervisorSubAgentSpecs {
		if name == spec.name {
			return true
		}
	}
	return false
}

func reasoningContent(message *schema.Message) string {
	if message == nil {
		return ""
	}
	if message.ReasoningContent != "" {
		return message.ReasoningContent
	}
	if value, ok := message.Extra["reasoning-content"].(string); ok && value != "" {
		return value
	}
	return ""
}

func toolProgressContent(toolName string) string {
	switch toolName {
	case domain.ToolProductSearch, domain.ToolProductDetail, domain.ToolProductRecommend:
		return "正在查询商品..."
	case domain.ToolInventoryGet:
		return "正在查询库存..."
	case domain.ToolCartList:
		return "正在查看购物车..."
	case domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete:
		return "正在处理购物车..."
	case domain.ToolCheckoutPrepare, domain.ToolCheckoutDetail:
		return "正在准备结算信息..."
	case domain.ToolOrderGet, domain.ToolOrderList:
		return "正在查询订单..."
	case domain.ToolOrderCreate, domain.ToolOrderCancel:
		return "正在处理订单..."
	case domain.ToolCouponList, domain.ToolCouponDetail, domain.ToolCouponMyList, domain.ToolCouponUsageList, domain.ToolCouponCalculate:
		return "正在查询优惠券..."
	case domain.ToolCouponClaim:
		return "正在领取优惠券..."
	default:
		return "正在处理请求..."
	}
}

func wrappedToolSummary(toolName, raw string) string {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data == nil {
		return defaultToolSummary(toolName)
	}
	if summary, ok := stringField(data, "summary"); ok && strings.TrimSpace(summary) != "" {
		return strings.TrimSpace(summary)
	}
	switch toolName {
	case domain.ToolProductSearch, domain.ToolProductRecommend:
		if products, ok := data["products"].([]any); ok {
			return fmt.Sprintf("找到 %d 件商品。", len(products))
		}
	case domain.ToolCartList:
		if total, ok := numberField(data, "total"); ok {
			return fmt.Sprintf("购物车共有 %d 件条目。", int(total))
		}
	case domain.ToolCheckoutPrepare:
		if id, ok := stringField(data, "pre_order_id"); ok && id != "" {
			return fmt.Sprintf("预结算已创建，预订单号为 %s。", id)
		}
	case domain.ToolOrderCreate:
		if order, ok := data["order"].(map[string]any); ok {
			if id, ok := stringField(order, "order_id"); ok && id != "" {
				return fmt.Sprintf("订单 %s 已创建。", id)
			}
		}
	case domain.ToolOrderCancel:
		return "订单已取消。"
	case domain.ToolCouponClaim:
		return "优惠券已领取。"
	}
	return defaultToolSummary(toolName)
}

func defaultToolSummary(toolName string) string {
	switch toolName {
	case domain.ToolProductSearch, domain.ToolProductDetail, domain.ToolProductRecommend:
		return "商品信息已查询完成。"
	case domain.ToolInventoryGet:
		return "库存信息已查询完成。"
	case domain.ToolCartList, domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete:
		return "购物车处理已完成。"
	case domain.ToolCheckoutPrepare, domain.ToolCheckoutDetail:
		return "结算信息已准备完成。"
	case domain.ToolOrderGet, domain.ToolOrderList, domain.ToolOrderCreate, domain.ToolOrderCancel:
		return "订单处理已完成。"
	case domain.ToolCouponList, domain.ToolCouponDetail, domain.ToolCouponClaim, domain.ToolCouponMyList, domain.ToolCouponUsageList, domain.ToolCouponCalculate:
		return "优惠券信息已处理完成。"
	default:
		return "工具调用已完成。"
	}
}

func isBusinessWriteTool(toolName string) bool {
	switch toolName {
	case domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCartDelete,
		domain.ToolOrderCreate, domain.ToolOrderCancel,
		domain.ToolCouponClaim:
		return true
	default:
		return false
	}
}

func stringField(data map[string]any, key string) (string, bool) {
	value, ok := data[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func numberField(data map[string]any, key string) (float64, bool) {
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func ensureJSONObject(value string) string {
	value = strings.TrimSpace(value)
	if jsonObjectLike(value) {
		return value
	}
	return fmt.Sprintf(`{"result":%q}`, value)
}

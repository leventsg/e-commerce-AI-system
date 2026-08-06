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
	req           RunRequest
	highRiskTools *aitools.HighRiskTools
	emit          func(context.Context, domain.AgentEvent) error

	mu                        sync.Mutex
	businessExecuted          bool
	emittedAny                bool
	assistantEmitted          bool
	visibleAssistantMessageID string
	assistantContent          strings.Builder
	assistantFinalEmitted     bool
	toolProgressKeys          map[string]bool
	toolResultKeys            map[string]bool
	activeToolMessageIDs      map[string][]string
}

func newAgentEventCallbackBridge(req RunRequest, highRiskTools *aitools.HighRiskTools, emit func(context.Context, domain.AgentEvent) error) *agentEventCallbackBridge {
	return &agentEventCallbackBridge{
		req:                  req,
		highRiskTools:        highRiskTools,
		emit:                 emit,
		toolProgressKeys:     make(map[string]bool),
		toolResultKeys:       make(map[string]bool),
		activeToolMessageIDs: make(map[string][]string),
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
	content := strings.TrimSpace(output.Message.Content)
	if content == "" || len(output.Message.ToolCalls) > 0 {
		return ctx
	}
	b.recordNonStreamingAssistantContent(content)
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
		if chunk == nil || chunk.Message == nil || len(chunk.Message.ToolCalls) > 0 {
			continue
		}
		text := chunk.Message.Content
		if text == "" {
			continue
		}
		messageID := b.assistantMessageID()
		// 汇总流式的 assistant chunk内容
		b.appendAssistantDelta(text)
		_ = b.send(ctx, domain.AgentEvent{
			Type:           domain.EventAssistantDelta,
			ConversationID: b.req.ConversationID,
			MessageID:      messageID,
			Content:        text,
			Done:           false,
		})
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
	event := domain.AgentEvent{
		Type:           domain.EventToolProgress,
		ConversationID: b.req.ConversationID,
		MessageID:      messageID,
		Tool:           toolName,
		Status:         "running",
		Content:        toolProgressContent(toolName),
		DataJSON:       ensureJSONObject(dataJSON),
		Done:           false,
	}
	if !b.markToolProgress(event) {
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
	event := domain.AgentEvent{
		Type:           domain.EventToolResult,
		ConversationID: b.req.ConversationID,
		MessageID:      b.finishToolMessageID(toolName),
		Tool:           toolName,
		Status:         "success",
		Content:        wrappedToolSummary(toolName, response),
		DataJSON:       ensureJSONObject(response),
		Done:           true,
	}
	if isBusinessWriteTool(toolName) || (b.highRiskTools != nil && b.highRiskTools.RequiresConfirmation(toolName)) {
		event.BusinessExecuted = true
	}
	if event.BusinessExecuted {
		b.mu.Lock()
		b.businessExecuted = true
		b.mu.Unlock()
	}
	if !b.markToolResult(event) {
		return ctx
	}
	_ = b.send(ctx, event)
	return ctx
}

func (b *agentEventCallbackBridge) onToolError(ctx context.Context, info *einocallbacks.RunInfo, err error) context.Context {
	toolName := info.Name
	if toolName == "" || isAgentToolName(toolName) || err == nil {
		return ctx
	}
	_ = b.send(ctx, domain.AgentEvent{
		Type:           domain.EventToolResult,
		ConversationID: b.req.ConversationID,
		MessageID:      b.finishToolMessageID(toolName),
		Tool:           toolName,
		Status:         "failed",
		Content:        "工具调用未完成，请稍后重试。",
		DataJSON:       fmt.Sprintf(`{"error":%q}`, err.Error()),
		Done:           true,
	})
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

func (b *agentEventCallbackBridge) markBusinessExecuted() {
	b.mu.Lock()
	b.businessExecuted = true
	b.mu.Unlock()
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

func (b *agentEventCallbackBridge) hasBufferedAssistantContent() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.assistantContent.String()) != "" && !b.assistantFinalEmitted
}

func (b *agentEventCallbackBridge) hasToolResult(event domain.AgentEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.toolResultKeys[toolResultDedupeKey(event)]
}

func (b *agentEventCallbackBridge) appendAssistantDelta(text string) {
	if text == "" {
		return
	}
	b.mu.Lock()
	b.assistantContent.WriteString(text)
	b.mu.Unlock()
}

func (b *agentEventCallbackBridge) recordNonStreamingAssistantContent(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	b.mu.Lock()
	if b.assistantContent.Len() == 0 {
		b.assistantContent.WriteString(content)
	}
	b.assistantMessageIDLocked()
	b.mu.Unlock()
}

func (b *agentEventCallbackBridge) finalAssistantEvent() (domain.AgentEvent, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	content := b.assistantContent.String()
	if content == "" || b.assistantFinalEmitted {
		return domain.AgentEvent{}, false
	}
	b.assistantFinalEmitted = true
	return domain.AgentEvent{
		Type:           domain.EventAssistantMessage,
		ConversationID: b.req.ConversationID,
		MessageID:      b.assistantMessageIDLocked(),
		Content:        content,
		Done:           true,
	}, true
}

func (b *agentEventCallbackBridge) assistantMessageID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.assistantMessageIDLocked()
}

func (b *agentEventCallbackBridge) assistantMessageIDLocked() string {
	if b.visibleAssistantMessageID == "" {
		b.visibleAssistantMessageID = newAgentMessageID()
	}
	return b.visibleAssistantMessageID
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

func (b *agentEventCallbackBridge) markToolProgress(event domain.AgentEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := toolProgressDedupeKey(event)
	if b.toolProgressKeys[key] {
		return false
	}
	b.toolProgressKeys[key] = true
	return true
}

func (b *agentEventCallbackBridge) markToolResult(event domain.AgentEvent) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := toolResultDedupeKey(event)
	if b.toolResultKeys[key] {
		return false
	}
	b.toolResultKeys[key] = true
	return true
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
	if b.req.OnEvent != nil && shouldEmitToOnEvent(event.Type) {
		if err := b.req.OnEvent(ctx, event); err != nil {
			return err
		}
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

func toolProgressDedupeKey(event domain.AgentEvent) string {
	return strings.TrimSpace(event.Tool)
}

func shouldEmitToOnEvent(eventType string) bool {
	return eventType != domain.EventAssistantDelta && eventType != domain.EventToolProgress
}

// 工具名称 + 状态 + 数据JSON + 内容 作为去重的key
func toolResultDedupeKey(event domain.AgentEvent) string {
	return strings.TrimSpace(event.Tool) + "|status:" + strings.TrimSpace(event.Status) +
		"|data:" + canonicalJSON(event.DataJSON) + "|content:" + strings.TrimSpace(event.Content)
}

func canonicalJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var data any
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return value
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return value
	}
	return string(encoded)
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

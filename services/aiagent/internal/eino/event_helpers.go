package eino

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func toolCallKey(toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID != "" {
		return toolCallID
	}
	return strings.TrimSpace(toolName)
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

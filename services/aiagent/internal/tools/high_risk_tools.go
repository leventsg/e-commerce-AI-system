package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
)

var (
	ErrConfirmationCreatorRequired = errors.New("confirmation creator required")
	ErrHighRiskToolExecution       = errors.New("high risk tool execution failed")
)

// ConfirmationCreator 创建确认请求记录
type ConfirmationCreator interface {
	Create(ctx context.Context, req confirmation.CreateRequest) (*domain.Confirmation, error)
}

type HighRiskToolClients struct {
	Cart     CartHighRiskRPC
	Order    OrderHighRiskRPC
	Checkout CheckoutQueryRPC
	Coupon   CouponCalculateRPC
}

// HighRiskTools 高风险工具管理器
type HighRiskTools struct {
	executor     *Executor
	creator      ConfirmationCreator
	checkout     CheckoutQueryRPC
	coupon       CouponCalculateRPC
	handlers     map[string]HandlerFunc                                           // 工具执行处理函数
	summaryFuncs map[string]func(context.Context, ExecuteRequest) (string, error) // 工具执行后的摘要函数
}

func NewHighRiskTools(executor *Executor, creator ConfirmationCreator, clients HighRiskToolClients) *HighRiskTools {
	handlers := make(map[string]HandlerFunc)
	mergeHandlers(handlers, cartHighRiskHandlers(clients.Cart))
	mergeHandlers(handlers, orderHighRiskHandlers(clients.Order))
	h := &HighRiskTools{
		executor: executor,
		creator:  creator,
		checkout: clients.Checkout,
		coupon:   clients.Coupon,
		handlers: handlers,
	}
	h.summaryFuncs = map[string]func(context.Context, ExecuteRequest) (string, error){
		domain.ToolCartDelete:  h.cartDeleteSummary,
		domain.ToolOrderCreate: h.orderCreateSummary,
		domain.ToolOrderCancel: h.orderCancelSummary,
	}
	// 绑定工具
	h.bindEinoTools()
	return h
}

// RequestConfirmation 请求确认
func (h *HighRiskTools) RequestConfirmation(ctx context.Context, req ExecuteRequest) (event domain.AgentEvent) {
	startedAt := time.Now()
	if h.executor == nil || h.executor.registry == nil {
		return failedToolEvent(req, req.ToolName, "确认请求暂不可用，请稍后重试。", ErrConfirmationCreatorRequired)
	}
	// 检查工具是否为高风险工具，是否需要确认
	metadata, err := h.executor.registry.Metadata(req.ToolName)
	// 只有 RiskHigh + RequireConfirmation + WriteOperation 的工具才需要确认
	if err != nil || metadata.Risk != domain.RiskHigh || !metadata.RequireConfirmation || !metadata.WriteOperation {
		if err == nil {
			err = confirmation.ErrConfirmationToolNotAllowed
		}
		return failedToolEvent(req, req.ToolName, "该操作不能进入确认流程。", err)
	}
	if h.creator == nil {
		return failedToolEvent(req, metadata.Name, "确认请求暂不可用，请稍后重试。", ErrConfirmationCreatorRequired)
	}
	// 对敏感参数进行脱敏处理
	args := argx.SanitizeMapKeys(req.Arguments, sensitiveToolArgumentKeys)
	req.Arguments = args
	defer func() {
		// 记录确认请求的执行结果
		recordMetadata := metadata
		recordMetadata.WriteOperation = false
		status := event.Status
		errMessage := ""
		if event.Type == domain.EventConfirmationRequired {
			status = toolStatusSuccess
		} else if status == toolStatusFailed {
			errMessage = event.Content
		}
		_ = h.executor.record(ctx, req, recordMetadata, args, status, event.Content, errMessage, event.DataJSON, time.Since(startedAt))
	}()
	// 生成确认请求的摘要
	summaryFn := h.summaryFuncs[metadata.Name]
	if summaryFn == nil {
		return failedToolEvent(req, metadata.Name, "该操作暂不支持确认。", ErrToolHandlerRequired)
	}
	summary, err := summaryFn(ctx, req)
	if err != nil {
		return failedToolEvent(req, metadata.Name, "无法创建确认请求，请检查操作参数。", err)
	}
	// 创建确认请求记录，存储数据
	created, err := h.creator.Create(ctx, confirmation.CreateRequest{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		ToolName:       metadata.Name,
		Arguments:      args,
		Summary:        summary,
	})
	if err != nil || created == nil {
		if err == nil {
			err = ErrConfirmationCreatorRequired
		}
		return failedToolEvent(req, metadata.Name, "确认请求创建失败，请稍后重试。", err)
	}
	payload := map[string]any{
		"type":              domain.EventConfirmationRequired,
		"confirmation_id":   created.ID,
		"action":            metadata.Name,
		"summary":           created.Summary,
		"expires_at":        created.ExpiresAt.Unix(),
		"arguments_summary": args,
	}
	return domain.AgentEvent{
		Type:           domain.EventConfirmationRequired,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Tool:           metadata.Name,
		Status:         confirmation.StatusPending,
		DataJSON:       marshalToolData(payload),
		Content:        created.Summary,
		ConfirmationID: created.ID,
		Action:         metadata.Name,
		Summary:        created.Summary,
		ExpiresAt:      created.ExpiresAt.Unix(),
		Done:           true,
	}
}

// ExecuteConfirmed 执行已确认的高风险工具操作（用户确认后执行）
func (h *HighRiskTools) ExecuteConfirmed(ctx context.Context, req ExecuteRequest) domain.AgentEvent {
	if h.executor == nil {
		return failedToolEvent(req, req.ToolName, "工具暂不可用，请稍后重试。", ErrToolHandlerRequired)
	}
	return h.executor.Execute(ctx, req, h.handlers[req.ToolName])
}

// cartDeleteSummary 生成删除购物车条目的确认摘要
func (h *HighRiskTools) cartDeleteSummary(_ context.Context, req ExecuteRequest) (string, error) {
	value, err := requiredInt64Argument(req.Arguments, "cart_item_id")
	if err != nil {
		return "", err
	}
	if _, err := positiveInt32(value, "cart_item_id"); err != nil {
		return "", err
	}
	return fmt.Sprintf("确认删除购物车条目 %d？", value), nil
}

// orderCancelSummary 生成取消订单的摘要
func (h *HighRiskTools) orderCancelSummary(_ context.Context, req ExecuteRequest) (string, error) {
	orderID, err := requiredStringArgument(req.Arguments, "order_id")
	if err != nil {
		return "", err
	}
	reason, err := optionalStringArgument(req.Arguments, "reason")
	if err != nil {
		return "", err
	}
	if reason == "" {
		return fmt.Sprintf("确认取消订单 %s？", orderID), nil
	}
	return fmt.Sprintf("确认取消订单 %s？取消原因为：%s。", orderID, reason), nil
}

// orderCreateSummary 生成创建订单的摘要
func (h *HighRiskTools) orderCreateSummary(ctx context.Context, req ExecuteRequest) (string, error) {
	// 解析参数
	preOrderID, err := requiredStringArgument(req.Arguments, "pre_order_id")
	if err != nil {
		return "", err
	}
	addressValue, err := requiredInt64Argument(req.Arguments, "address_id")
	if err != nil {
		return "", err
	}
	if _, err := positiveInt32(addressValue, "address_id"); err != nil {
		return "", err
	}
	paymentValue, err := requiredInt64Argument(req.Arguments, "payment_method")
	if err != nil {
		return "", err
	}
	if paymentValue != 1 && paymentValue != 2 {
		return "", invalidArgument("payment_method", "must be 1 or 2")
	}
	if h.checkout == nil {
		return "", ErrToolHandlerRequired
	}
	userID, err := authenticatedUserID32(req.UserID)
	if err != nil {
		return "", err
	}
	// 获取预订单详情
	resp, err := h.checkout.GetCheckoutDetail(ctx, &checkoutservice.CheckoutDetailReq{PreOrderId: preOrderID, UserId: userID})
	if err != nil {
		return "", fmt.Errorf("checkout_detail before order_create: %w", err)
	}
	if resp == nil || resp.Data == nil {
		return "", fmt.Errorf("checkout_detail before order_create returned empty checkout")
	}
	if err := validateRPCResponse("checkout_detail before order_create", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
		return "", err
	}
	if resp.Data.UserId != int64(userID) {
		return "", fmt.Errorf("checkout_detail before order_create does not belong to authenticated user")
	}
	quantity := int64(0)
	for _, item := range resp.Data.Items {
		if item != nil {
			quantity += int64(item.Quantity)
		}
	}
	summary := fmt.Sprintf("确认使用预订单 %s 创建订单？应付金额 %d 分，商品数量 %d。", preOrderID, resp.Data.FinalAmount, quantity)
	couponID, err := optionalStringArgument(req.Arguments, "coupon_id")
	if err != nil {
		return "", err
	}
	if couponID != "" {
		if h.coupon == nil {
			return "", ErrToolHandlerRequired
		}
		items := make([]*couponsclient.Items, 0, len(resp.Data.Items))
		for _, item := range resp.Data.Items {
			if item != nil {
				items = append(items, &couponsclient.Items{ProductId: item.ProductId, Quantity: item.Quantity})
			}
		}
		// 计算使用优惠券的最终金额
		calculated, err := h.coupon.CalculateCoupon(ctx, &couponsclient.CalculateCouponReq{
			UserId: userID, CouponId: couponID, Items: items,
		})
		if err != nil {
			return "", fmt.Errorf("coupon_calculate before order_create: %w", err)
		}
		if calculated == nil {
			return "", fmt.Errorf("coupon_calculate before order_create returned nil response")
		}
		if err := validateRPCResponse("coupon_calculate before order_create", calculated, int64(calculated.StatusCode), calculated.StatusMsg); err != nil {
			return "", err
		}
		if !calculated.IsUsable {
			return "", invalidArgument("coupon_id", "is not usable for the checkout items")
		}
		summary = fmt.Sprintf("确认使用预订单 %s 和优惠券 %s 创建订单？应付金额 %d 分，商品数量 %d。", preOrderID, couponID, calculated.FinalAmount, quantity)
	}
	return summary, nil
}

// 绑定eino工具
func (h *HighRiskTools) bindEinoTools() {
	if h.executor == nil || h.executor.registry == nil {
		return
	}
	for name := range h.summaryFuncs {
		base, err := h.executor.registry.Tool(name)
		if err != nil {
			continue
		}
		h.executor.registry.tools[name] = &highRiskInvokableTool{name: name, base: base, tools: h}
	}
}

// Eino工具包装器，实现工具调用接口
type highRiskInvokableTool struct {
	name  string
	base  einotool.InvokableTool
	tools *HighRiskTools
}

func (t *highRiskInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

func (t *highRiskInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	execution, ok := ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 || strings.TrimSpace(execution.ConversationID) == "" {
		return "", ErrToolExecutionContext
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("%w: invalid JSON arguments: %v", ErrInvalidToolArguments, err)
	}
	event := t.tools.RequestConfirmation(ctx, executeRequestFromContext(execution, t.name, args))
	if event.Type != domain.EventConfirmationRequired {
		return event.DataJSON, fmt.Errorf("%w: %s", ErrHighRiskToolExecution, event.Content)
	}
	return event.DataJSON, nil
}

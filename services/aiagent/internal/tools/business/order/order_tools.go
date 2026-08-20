package order_tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"google.golang.org/grpc"
)

type OrderQueryRPC interface {
	GetOrder(ctx context.Context, in *orderservice.GetOrderRequest, opts ...grpc.CallOption) (*orderservice.OrderDetailResponse, error)
	ListOrders(ctx context.Context, in *orderservice.ListOrdersRequest, opts ...grpc.CallOption) (*orderservice.ListOrdersResponse, error)
}

type OrderHighRiskRPC interface {
	CancelOrder(ctx context.Context, in *orderservice.CancelOrderRequest, opts ...grpc.CallOption) (*orderservice.EmptyRes, error)
}

type OrderCreateAPI interface {
	CreateOrder(ctx context.Context, req CreateOrderHTTPRequest) (*CreateOrderHTTPResponse, error)
}

func OrderQueryHandlers(rpc OrderQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolOrderGet:  orderGetHandler(rpc),
		domain.ToolOrderList: orderListHandler(rpc),
	}
}

func OrderHighRiskHandlers(createAPI OrderCreateAPI, cancelRPC OrderHighRiskRPC) map[string]core.HandlerFunc {
	if createAPI == nil && cancelRPC == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolOrderCreate: orderCreateHandler(createAPI),
		domain.ToolOrderCancel: orderCancelHandler(cancelRPC),
	}
}

// 创建订单工具处理函数
func orderCreateHandler(api OrderCreateAPI) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		if api == nil {
			return core.HandlerResult{}, fmt.Errorf("order_create handler unavailable")
		}
		// 解析参数
		_, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		preOrderID, err := helper.RequiredStringArgument(req.Arguments, "pre_order_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		couponID, err := helper.OptionalStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		addressValue, err := helper.RequiredInt64Argument(req.Arguments, "address_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		addressID, err := helper.PositiveInt32(addressValue, "address_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		// 调用 order-api 创建订单，由 API 完成优惠券锁定、结算锁定和订单创建 saga。
		resp, err := api.CreateOrder(ctx, CreateOrderHTTPRequest{
			PreOrderID:    preOrderID,
			CouponID:      couponID,
			AddressID:     addressID,
			PaymentMethod: PaymentMethodAlipay,
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("order_create http: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("order_create returned nil response")
		}
		if strings.TrimSpace(resp.Order.OrderID) == "" {
			return core.HandlerResult{}, fmt.Errorf("order_create returned empty order")
		}
		return core.HandlerResult{
			Data: map[string]any{
				"order": resp.Order,
				"items": resp.Items,
			},
			Summary: fmt.Sprintf("订单 %s 已创建，支付方式为支付宝。", resp.Order.OrderID),
		}, nil
	}
}

func orderCancelHandler(rpc OrderHighRiskRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		orderID, err := helper.RequiredStringArgument(req.Arguments, "order_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		reason, err := helper.OptionalStringArgument(req.Arguments, "reason")
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.CancelOrder(ctx, &orderservice.CancelOrderRequest{
			OrderId:      orderID,
			UserId:       uint32(userID),
			CancelReason: reason,
			Initiative:   true,
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("order_cancel rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("order_cancel returned nil response")
		}
		if err := helper.ValidateRPCResponse("order_cancel", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data:    map[string]any{"order_id": orderID, "reason": reason},
			Summary: fmt.Sprintf("订单 %s 已取消。", orderID),
		}, nil
	}
}

// 订单查询：获取订单详情
func orderGetHandler(rpc OrderQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		orderID, err := helper.RequiredStringArgument(req.Arguments, "order_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetOrder(ctx, &orderservice.GetOrderRequest{OrderId: orderID, UserId: uint32(userID)})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("order_get rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: order_get returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("order_get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Order == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: order_get returned empty order", helper.ErrQueryRPCUnavailable)
		}
		data := map[string]any{
			"order": compactOrder(resp.Order),
			"items": compactOrderItems(resp.Items),
		}
		if resp.Address != nil {
			data["address"] = compactOrderAddress(resp.Address)
		}
		return core.HandlerResult{
			Data:    data,
			Summary: fmt.Sprintf("订单 %s 当前状态为 %s。", orderID, resp.Order.OrderStatus.String()),
		}, nil
	}
}

func orderListHandler(rpc OrderQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		page, pageSize, err := helper.QueryPagination(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		statusText, err := helper.OptionalStringArgument(req.Arguments, "status")
		if err != nil {
			return core.HandlerResult{}, err
		}
		statuses, err := parseOrderStatuses(statusText)
		if err != nil {
			return core.HandlerResult{}, err
		}

		resp, err := rpc.ListOrders(ctx, &orderservice.ListOrdersRequest{
			UserId: uint32(userID),
			StatusFilter: &orderservice.ListOrdersRequest_OrderStatusFilter{
				Statuses: statuses,
			},
			Pagination: &orderservice.ListOrdersRequest_Pagination{Page: page, PageSize: pageSize},
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("order_list rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: order_list returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("order_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		orders := make([]map[string]any, 0, len(resp.Orders))
		for _, item := range resp.Orders {
			if item != nil {
				orders = append(orders, compactOrder(item))
			}
		}
		return core.HandlerResult{
			Data: map[string]any{
				"count":     len(orders),
				"page":      page,
				"page_size": pageSize,
				"orders":    orders,
			},
			Summary: fmt.Sprintf("查询到 %d 个订单。", len(orders)),
		}, nil
	}
}

func parseOrderStatuses(value string) ([]order.OrderStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	aliases := map[string]order.OrderStatus{
		"created":         order.OrderStatus_ORDER_STATUS_CREATED,
		"pending_payment": order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT,
		"pending":         order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT,
		"paid":            order.OrderStatus_ORDER_STATUS_PAID,
		"completed":       order.OrderStatus_ORDER_STATUS_COMPLETED,
		"cancelled":       order.OrderStatus_ORDER_STATUS_CANCELLED,
		"canceled":        order.OrderStatus_ORDER_STATUS_CANCELLED,
		"closed":          order.OrderStatus_ORDER_STATUS_CLOSED,
		"refund":          order.OrderStatus_ORDER_STATUS_REFUND,
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' })
	result := make([]order.OrderStatus, 0, len(parts))
	for _, part := range parts {
		key := strings.ToLower(strings.TrimSpace(part))
		if status, ok := aliases[key]; ok {
			result = append(result, status)
			continue
		}
		protoKey := strings.ToUpper(key)
		if !strings.HasPrefix(protoKey, "ORDER_STATUS_") {
			protoKey = "ORDER_STATUS_" + protoKey
		}
		value, ok := order.OrderStatus_value[protoKey]
		if !ok || value == int32(order.OrderStatus_ORDER_STATUS_UNSPECIFIED) {
			return nil, helper.InvalidArgument("status", "contains an unsupported order status")
		}
		result = append(result, order.OrderStatus(value))
	}
	return result, nil
}

func compactOrder(value *orderservice.Order) map[string]any {
	return map[string]any{
		"order_id":        value.OrderId,
		"pre_order_id":    value.PreOrderId,
		"original_amount": value.OriginalAmount,
		"discount_amount": value.DiscountAmount,
		"payable_amount":  value.PayableAmount,
		"paid_amount":     value.PaidAmount,
		"order_status":    value.OrderStatus.String(),
		"payment_status":  value.PaymentStatus.String(),
		"payment_method":  value.PaymentMethod.String(),
		"reason":          value.Reason,
		"expire_time":     value.ExpireTime,
		"created_at":      value.CreatedAt,
		"updated_at":      value.UpdatedAt,
	}
}

func compactOrderItems(items []*orderservice.OrderItem) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, map[string]any{
			"product_id":   item.ProductId,
			"product_name": item.ProductName,
			"quantity":     item.Quantity,
			"unit_price":   item.UnitPrice,
		})
	}
	return result
}

func compactOrderAddress(address *orderservice.OrderAddress) map[string]any {
	return map[string]any{
		"recipient_name":   address.RecipientName,
		"phone_number":     address.PhoneNumber,
		"province":         address.Province,
		"city":             address.City,
		"detailed_address": address.DetailedAddress,
	}
}

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"google.golang.org/grpc"
)

type OrderQueryRPC interface {
	GetOrder(ctx context.Context, in *orderservice.GetOrderRequest, opts ...grpc.CallOption) (*orderservice.OrderDetailResponse, error)
	ListOrders(ctx context.Context, in *orderservice.ListOrdersRequest, opts ...grpc.CallOption) (*orderservice.ListOrdersResponse, error)
}

func orderQueryHandlers(rpc OrderQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolOrderGet:  orderGetHandler(rpc),
		domain.ToolOrderList: orderListHandler(rpc),
	}
}

// 订单查询：获取订单详情
func orderGetHandler(rpc OrderQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		orderID, err := requiredStringArgument(req.Arguments, "order_id")
		if err != nil {
			return HandlerResult{}, err
		}
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.GetOrder(ctx, &orderservice.GetOrderRequest{OrderId: orderID, UserId: uint32(userID)})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("order.get rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: order.get returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("order.get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		if resp.Order == nil {
			return HandlerResult{}, fmt.Errorf("%w: order.get returned empty order", ErrQueryRPCUnavailable)
		}
		data := map[string]any{
			"order": compactOrder(resp.Order),
			"items": compactOrderItems(resp.Items),
		}
		if resp.Address != nil {
			data["address"] = compactOrderAddress(resp.Address)
		}
		return HandlerResult{
			Data:    data,
			Summary: fmt.Sprintf("订单 %s 当前状态为 %s。", orderID, resp.Order.OrderStatus.String()),
		}, nil
	}
}

func orderListHandler(rpc OrderQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		statusText, err := optionalStringArgument(req.Arguments, "status")
		if err != nil {
			return HandlerResult{}, err
		}
		statuses, err := parseOrderStatuses(statusText)
		if err != nil {
			return HandlerResult{}, err
		}

		resp, err := rpc.ListOrders(ctx, &orderservice.ListOrdersRequest{
			UserId: uint32(userID),
			StatusFilter: &orderservice.ListOrdersRequest_OrderStatusFilter{
				Statuses: statuses,
			},
			Pagination: &orderservice.ListOrdersRequest_Pagination{Page: page, PageSize: pageSize},
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("order.list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: order.list returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("order.list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		orders := make([]map[string]any, 0, len(resp.Orders))
		for _, item := range resp.Orders {
			if item != nil {
				orders = append(orders, compactOrder(item))
			}
		}
		return HandlerResult{
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
			return nil, invalidArgument("status", "contains an unsupported order status")
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

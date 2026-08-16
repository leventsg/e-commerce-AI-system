package checkout_tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
)

type CheckoutQueryRPC interface {
	GetCheckoutDetail(ctx context.Context, in *checkoutservice.CheckoutDetailReq, opts ...grpc.CallOption) (*checkoutservice.CheckoutDetailResp, error)
}

type CheckoutWriteRPC interface {
	PrepareCheckout(ctx context.Context, in *checkoutservice.CheckoutReq, opts ...grpc.CallOption) (*checkoutservice.CheckoutResp, error)
}

func CheckoutQueryHandlers(rpc CheckoutQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolCheckoutDetail: checkoutDetailHandler(rpc),
	}
}

func CheckoutWriteHandlers(rpc CheckoutWriteRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolCheckoutPrepare: checkoutPrepareHandler(rpc),
	}
}

func checkoutPrepareHandler(rpc CheckoutWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		items, err := checkoutItemsArgument(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		couponID, err := helper.OptionalStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.PrepareCheckout(ctx, &checkoutservice.CheckoutReq{
			UserId:     uint32(userID),
			CouponId:   couponID,
			OrderItems: items,
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("checkout_prepare rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: checkout_prepare returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("checkout_prepare", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data: map[string]any{
				"pre_order_id": resp.PreOrderId,
				"expire_time":  resp.ExpireTime,
				"pay_methods":  resp.PayMethod,
			},
			Summary: fmt.Sprintf("预结算已创建，预订单号为 %s。", resp.PreOrderId),
		}, nil
	}
}

func checkoutItemsArgument(args map[string]any) ([]*checkoutservice.CheckoutReq_OrderItem, error) {
	value, ok := args["order_items"]
	if !ok {
		return nil, helper.InvalidArgument("order_items", "is required")
	}
	rawItems, ok := value.([]any)
	if !ok || len(rawItems) == 0 {
		return nil, helper.InvalidArgument("order_items", "must be a non-empty array")
	}
	items := make([]*checkoutservice.CheckoutReq_OrderItem, 0, len(rawItems))
	for _, raw := range rawItems {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, helper.InvalidArgument("order_items", "must contain objects")
		}
		productValue, err := helper.RequiredInt64Argument(object, "product_id")
		if err != nil {
			return nil, err
		}
		quantityValue, err := helper.RequiredInt64Argument(object, "quantity")
		if err != nil {
			return nil, err
		}
		productID, err := helper.PositiveInt32(productValue, "product_id")
		if err != nil {
			return nil, err
		}
		quantity, err := helper.PositiveInt32(quantityValue, "quantity")
		if err != nil {
			return nil, err
		}
		items = append(items, &checkoutservice.CheckoutReq_OrderItem{ProductId: productID, Quantity: quantity})
	}
	return items, nil
}

// 结算查询：获取预订单结算详情
func checkoutDetailHandler(rpc CheckoutQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		preOrderID, err := helper.RequiredStringArgument(req.Arguments, "pre_order_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetCheckoutDetail(ctx, &checkoutservice.CheckoutDetailReq{
			PreOrderId: preOrderID,
			UserId:     userID,
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("checkout_detail rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: checkout_detail returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("checkout_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Data == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: checkout_detail returned empty checkout", helper.ErrQueryRPCUnavailable)
		}
		items := make([]map[string]any, 0, len(resp.Data.Items))
		for _, item := range resp.Data.Items {
			if item == nil {
				continue
			}
			items = append(items, map[string]any{
				"product_id":   item.ProductId,
				"product_name": item.ProductName,
				"quantity":     item.Quantity,
				"price":        item.Price,
			})
		}
		return core.HandlerResult{
			Data: map[string]any{
				"pre_order_id":    resp.Data.PreOrderId,
				"status":          resp.Data.Status.String(),
				"expire_time":     resp.Data.ExpireTime,
				"original_amount": resp.Data.OriginalAmount,
				"final_amount":    resp.Data.FinalAmount,
				"items":           items,
				"created_at":      resp.Data.CreatedAt,
				"updated_at":      resp.Data.UpdatedAt,
			},
			Summary: fmt.Sprintf("已查询预订单 %s 的结算详情。", preOrderID),
		}, nil
	}
}

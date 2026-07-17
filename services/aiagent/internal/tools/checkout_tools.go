package tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"google.golang.org/grpc"
)

type CheckoutQueryRPC interface {
	GetCheckoutDetail(ctx context.Context, in *checkoutservice.CheckoutDetailReq, opts ...grpc.CallOption) (*checkoutservice.CheckoutDetailResp, error)
}

func checkoutQueryHandlers(rpc CheckoutQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCheckoutDetail: checkoutDetailHandler(rpc),
	}
}

// 结算查询：获取预订单结算详情
func checkoutDetailHandler(rpc CheckoutQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		preOrderID, err := requiredStringArgument(req.Arguments, "pre_order_id")
		if err != nil {
			return HandlerResult{}, err
		}
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.GetCheckoutDetail(ctx, &checkoutservice.CheckoutDetailReq{
			PreOrderId: preOrderID,
			UserId:     userID,
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("checkout.detail rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: checkout.detail returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("checkout.detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		if resp.Data == nil {
			return HandlerResult{}, fmt.Errorf("%w: checkout.detail returned empty checkout", ErrQueryRPCUnavailable)
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
		return HandlerResult{
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

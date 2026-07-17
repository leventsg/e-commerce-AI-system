package tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"google.golang.org/grpc"
)

type CartQueryRPC interface {
	CartItemList(ctx context.Context, in *cartsclient.UserInfo, opts ...grpc.CallOption) (*cartsclient.CartItemListResponse, error)
}

func cartQueryHandlers(rpc CartQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCartList: cartListHandler(rpc),
	}
}

// 购物车查询：获取用户购物车列表
func cartListHandler(rpc CartQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.CartItemList(ctx, &cartsclient.UserInfo{Id: userID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("cart.list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: cart.list returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("cart.list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		pageItems := paginateCartItems(resp.Data, page, pageSize)
		items := make([]map[string]any, 0, len(pageItems))
		for _, item := range pageItems {
			if item == nil {
				continue
			}
			items = append(items, map[string]any{
				"cart_item_id": item.Id,
				"product_id":   item.ProductId,
				"quantity":     item.Quantity,
				"checked":      item.Checked,
			})
		}
		total := resp.Total
		if total == 0 && len(resp.Data) > 0 {
			total = int32(len(resp.Data))
		}
		return HandlerResult{
			Data: map[string]any{
				"total":     total,
				"page":      page,
				"page_size": pageSize,
				"items":     items,
			},
			Summary: fmt.Sprintf("购物车共有 %d 件条目。", total),
		}, nil
	}
}

func paginateCartItems(items []*cartsclient.CartInfoResponse, page, pageSize int32) []*cartsclient.CartInfoResponse {
	start := int64(page-1) * int64(pageSize)
	if start >= int64(len(items)) {
		return []*cartsclient.CartInfoResponse{}
	}
	end := start + int64(pageSize)
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	return items[int(start):int(end)]
}

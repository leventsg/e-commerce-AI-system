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

type CartWriteRPC interface {
	CartItemList(ctx context.Context, in *cartsclient.UserInfo, opts ...grpc.CallOption) (*cartsclient.CartItemListResponse, error)
	CreateCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.CreateCartResponse, error)
	SubCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.SubCartResponse, error)
}

type CartHighRiskRPC interface {
	CartItemList(ctx context.Context, in *cartsclient.UserInfo, opts ...grpc.CallOption) (*cartsclient.CartItemListResponse, error)
	DeleteCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.EmptyCartResponse, error)
}

func cartQueryHandlers(rpc CartQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCartList: cartListHandler(rpc),
	}
}

func cartWriteHandlers(rpc CartWriteRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCartAdd: cartAddHandler(rpc),
		domain.ToolCartSub: cartSubHandler(rpc),
	}
}

func cartHighRiskHandlers(rpc CartHighRiskRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCartDelete: cartDeleteHandler(rpc),
	}
}

// 删除购物车条目工具处理函数
func cartDeleteHandler(rpc CartHighRiskRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		cartItemValue, err := requiredInt64Argument(req.Arguments, "cart_item_id")
		if err != nil {
			return HandlerResult{}, err
		}
		cartItemID, err := positiveInt32(cartItemValue, "cart_item_id")
		if err != nil {
			return HandlerResult{}, err
		}
		listResp, err := rpc.CartItemList(ctx, &cartsclient.UserInfo{Id: userID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("cart.delete list rpc: %w", err)
		}
		if listResp == nil {
			return HandlerResult{}, fmt.Errorf("cart.delete list returned nil response")
		}
		if err := validateRPCResponse("cart.delete list", listResp, int64(listResp.StatusCode), listResp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		item := ownedCartItem(listResp.Data, cartItemID, userID)
		if item == nil {
			return HandlerResult{}, invalidArgument("cart_item_id", "does not belong to authenticated user")
		}
		resp, err := rpc.DeleteCartItem(ctx, &cartsclient.CartItemRequest{
			Id:        cartItemID,
			UserId:    userID,
			ProductId: item.ProductId,
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("cart.delete rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("cart.delete returned nil response")
		}
		if err := validateRPCResponse("cart.delete", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		return HandlerResult{
			Data:    map[string]any{"cart_item_id": cartItemID, "product_id": item.ProductId},
			Summary: fmt.Sprintf("购物车条目 %d 已删除。", cartItemID),
		}, nil
	}
}

// cartAddHandler 添加商品到购物车工具处理函数
func cartAddHandler(rpc CartWriteRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 解析参数
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		productValue, err := requiredInt64Argument(req.Arguments, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		productID, err := positiveInt32(productValue, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		quantity, err := writeQuantity(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}

		var cartItemID int32
		// 循环创建购物车项，每个项增加数量
		// TODO: 可以优化为一次性创建购物车项并设置数量，而不是循环调用
		for completed := int32(0); completed < quantity; completed++ {
			// 调用 RPC 创建购物车项
			resp, callErr := rpc.CreateCartItem(ctx, &cartsclient.CartItemRequest{
				UserId:    userID,
				ProductId: productID,
				Quantity:  0,
			})
			if callErr != nil {
				return HandlerResult{}, fmt.Errorf("cart.add completed %d of %d units: %w", completed, quantity, callErr)
			}
			if resp == nil {
				return HandlerResult{}, fmt.Errorf("cart.add completed %d of %d units: nil response", completed, quantity)
			}
			if err := validateRPCResponse("cart.add", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
				return HandlerResult{}, fmt.Errorf("cart.add completed %d of %d units: %w", completed, quantity, err)
			}
			cartItemID = resp.Id
		}

		return HandlerResult{
			Data: map[string]any{
				"cart_item_id":   cartItemID,
				"product_id":     productID,
				"added_quantity": quantity,
			},
			Summary: fmt.Sprintf("已将商品 %d 加入购物车，数量增加 %d。", productID, quantity),
		}, nil
	}
}

// cartSubHandler 删除商品从购物车工具处理函数
func cartSubHandler(rpc CartWriteRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 解析参数
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		cartItemValue, err := requiredInt64Argument(req.Arguments, "cart_item_id")
		if err != nil {
			return HandlerResult{}, err
		}
		cartItemID, err := positiveInt32(cartItemValue, "cart_item_id")
		if err != nil {
			return HandlerResult{}, err
		}
		quantity, err := writeQuantity(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}

		// 获取该用户的购物车列表
		resp, err := rpc.CartItemList(ctx, &cartsclient.UserInfo{Id: userID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("cart.sub list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("cart.sub list returned nil response")
		}
		if err := validateRPCResponse("cart.sub list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		// 检查购物车项是否属于该用户
		item := ownedCartItem(resp.Data, cartItemID, userID)
		if item == nil {
			return HandlerResult{}, invalidArgument("cart_item_id", "does not belong to authenticated user")
		}
		if quantity >= item.Quantity {
			return HandlerResult{}, invalidArgument("quantity", "would remove the cart item; use cart.delete with confirmation")
		}

		// 循环减少购物车项的数量
		// TODO: 可以优化为一次性减少购物车项并设置数量，而不是循环调用
		for completed := int32(0); completed < quantity; completed++ {
			result, callErr := rpc.SubCartItem(ctx, &cartsclient.CartItemRequest{
				Id:        cartItemID,
				UserId:    userID,
				ProductId: item.ProductId,
			})
			if callErr != nil {
				return HandlerResult{}, fmt.Errorf("cart.sub completed %d of %d units: %w", completed, quantity, callErr)
			}
			if result == nil {
				return HandlerResult{}, fmt.Errorf("cart.sub completed %d of %d units: nil response", completed, quantity)
			}
			if err := validateRPCResponse("cart.sub", result, int64(result.StatusCode), result.StatusMsg); err != nil {
				return HandlerResult{}, fmt.Errorf("cart.sub completed %d of %d units: %w", completed, quantity, err)
			}
		}

		remaining := item.Quantity - quantity
		return HandlerResult{
			Data: map[string]any{
				"cart_item_id":        cartItemID,
				"product_id":          item.ProductId,
				"subtracted_quantity": quantity,
				"remaining_quantity":  remaining,
			},
			Summary: fmt.Sprintf("购物车商品数量已减少 %d，剩余 %d。", quantity, remaining),
		}, nil
	}
}

// writeQuantity 解析并验证购物车操作的数量参数，确保数量在1-100之间
func writeQuantity(args map[string]any) (int32, error) {
	value, err := requiredInt64Argument(args, "quantity")
	if err != nil {
		return 0, err
	}
	quantity, err := positiveInt32(value, "quantity")
	if err != nil {
		return 0, err
	}
	if quantity > 100 {
		return 0, invalidArgument("quantity", "must not exceed 100")
	}
	return quantity, nil
}

// ownedCartItem 检查并返回用户拥有的购物车项
func ownedCartItem(items []*cartsclient.CartInfoResponse, cartItemID, userID int32) *cartsclient.CartInfoResponse {
	for _, item := range items {
		if item != nil && item.Id == cartItemID && item.UserId == userID {
			return item
		}
	}
	return nil
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

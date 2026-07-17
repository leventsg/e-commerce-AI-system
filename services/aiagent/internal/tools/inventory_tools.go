package tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"google.golang.org/grpc"
)

type InventoryQueryRPC interface {
	GetInventory(ctx context.Context, in *inventoryclient.GetInventoryReq, opts ...grpc.CallOption) (*inventoryclient.GetInventoryResp, error)
}

func inventoryQueryHandlers(rpc InventoryQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolInventoryGet: inventoryGetHandler(rpc),
	}
}

// 库存查询：获取商品库存信息
func inventoryGetHandler(rpc InventoryQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		productIDValue, err := requiredInt64Argument(req.Arguments, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		productID, err := positiveInt32(productIDValue, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.GetInventory(ctx, &inventoryclient.GetInventoryReq{ProductId: productID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("inventory.get rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: inventory.get returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("inventory.get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		return HandlerResult{
			Data: map[string]any{
				"product_id": productID,
				"inventory":  resp.Inventory,
				"sold_count": resp.SoldCount,
			},
			Summary: fmt.Sprintf("商品 %d 当前库存为 %d。", productID, resp.Inventory),
		}, nil
	}
}

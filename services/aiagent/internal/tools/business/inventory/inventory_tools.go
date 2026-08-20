package inventory_tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"google.golang.org/grpc"
)

type InventoryQueryRPC interface {
	GetInventory(ctx context.Context, in *inventoryclient.GetInventoryReq, opts ...grpc.CallOption) (*inventoryclient.GetInventoryResp, error)
}

func InventoryQueryHandlers(rpc InventoryQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolInventoryGet: inventoryGetHandler(rpc),
	}
}

// 库存查询：获取商品库存信息
func inventoryGetHandler(rpc InventoryQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		productIDValue, err := helper.RequiredInt64Argument(req.Arguments, "product_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		productID, err := helper.PositiveInt32(productIDValue, "product_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetInventory(ctx, &inventoryclient.GetInventoryReq{ProductId: productID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("inventory_get rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: inventory_get returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("inventory_get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data: map[string]any{
				"product_id": productID,
				"inventory":  resp.Inventory,
				"sold_count": resp.SoldCount,
			},
			Summary: fmt.Sprintf("商品 %d 当前库存为 %d。", productID, resp.Inventory),
		}, nil
	}
}

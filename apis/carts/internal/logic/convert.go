package logic

import (
	"github.com/leventsg/e-commerce-AI-system/apis/carts/internal/types"
	"github.com/leventsg/e-commerce-AI-system/services/carts/carts"
)

func ConvertCartInfoResponse(res []*carts.CartInfoResponse) []*types.CartInfoResponse {
	var result []*types.CartInfoResponse
	for _, item := range res {
		result = append(result, &types.CartInfoResponse{
			Id:        item.Id,
			UserId:    item.UserId,
			ProductId: item.ProductId,
			Quantity:  item.Quantity,
		})
	}
	return result
}

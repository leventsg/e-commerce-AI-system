package logic

import (
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/utils/shopping"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"time"
)

func convertCheckout2Resp(data *checkout.CheckoutOrder) types.CheckoutOrder {
	return types.CheckoutOrder{
		PreOrderID:     data.PreOrderId,
		UserID:         data.UserId,
		Status:         int32(data.Status),
		ExpireTime:     time.Unix(data.ExpireTime, 0).Format(time.DateTime),
		CreatedAt:      data.CreatedAt,
		UpdatedAt:      data.UpdatedAt,
		Items:          convertCheckoutItem2Resp(data.Items),
		OriginalAmount: shopping.FenToYuan(data.OriginalAmount),
		FinalAmount:    shopping.FenToYuan(data.FinalAmount),
	}
}

func convertCheckoutItem2Resp(items []*checkout.CheckoutItem) []types.CheckoutItem {
	checkoutItems := make([]types.CheckoutItem, len(items))
	for i, item := range items {
		checkoutItems[i] = types.CheckoutItem{
			ProductID:   item.ProductId,
			Quantity:    item.Quantity,
			Price:       shopping.FenToYuan(item.Price),
			ProductName: item.ProductName,
			ProductDesc: item.ProductDesc,
		}
	}
	return checkoutItems
}

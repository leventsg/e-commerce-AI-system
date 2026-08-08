package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
)

type confirmationSummaryBuilder struct {
	cart     CartHighRiskRPC
	product  ProductQueryRPC
	checkout CheckoutQueryRPC
	coupon   CouponCalculateRPC
}

func (b *confirmationSummaryBuilder) cartDeleteSummary(ctx context.Context, req ExecuteRequest) (string, error) {
	value, err := requiredInt64Argument(req.Arguments, "cart_item_id")
	if err != nil {
		return "", err
	}
	cartItemID, err := positiveInt32(value, "cart_item_id")
	if err != nil {
		return "", err
	}
	userID, err := authenticatedUserID32(req.UserID)
	if err != nil {
		return "", err
	}
	if b.cart == nil {
		return fmt.Sprintf("确认从购物车删除条目 %d？", cartItemID), nil
	}
	listResp, err := b.cart.CartItemList(ctx, &cartsclient.UserInfo{Id: userID})
	if err != nil {
		return "", fmt.Errorf("cart_delete summary list rpc: %w", err)
	}
	if listResp == nil {
		return "", fmt.Errorf("cart_delete summary list returned nil response")
	}
	if err := validateRPCResponse("cart_delete summary list", listResp, int64(listResp.StatusCode), listResp.StatusMsg); err != nil {
		return "", err
	}
	item := ownedCartItem(listResp.Data, cartItemID, userID)
	if item == nil {
		return "", invalidArgument("cart_item_id", "does not belong to authenticated user")
	}
	productLabel := fmt.Sprintf("商品 %d", item.ProductId)
	if productName := b.cartDeleteProductName(ctx, uint32(item.ProductId), userID); productName != "" {
		productLabel = productName
	}
	req.Arguments["product_id"] = item.ProductId
	req.Arguments["product_name"] = productLabel
	req.Arguments["quantity"] = item.Quantity
	return fmt.Sprintf("确认从购物车删除 %s（数量 %d）？", productLabel, item.Quantity), nil
}

func (b *confirmationSummaryBuilder) cartDeleteProductName(ctx context.Context, productID uint32, userID int32) string {
	if b.product == nil || productID == 0 {
		return ""
	}
	resp, err := b.product.GetProduct(ctx, &productcatalogservice.GetProductReq{Id: productID, UserId: userID})
	if err != nil || resp == nil {
		return ""
	}
	if err := validateRPCResponse("cart_delete summary product_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
		return ""
	}
	if resp.Product == nil {
		return ""
	}
	return strings.TrimSpace(resp.Product.Name)
}

func (b *confirmationSummaryBuilder) orderCancelSummary(_ context.Context, req ExecuteRequest) (string, error) {
	orderID, err := requiredStringArgument(req.Arguments, "order_id")
	if err != nil {
		return "", err
	}
	reason, err := optionalStringArgument(req.Arguments, "reason")
	if err != nil {
		return "", err
	}
	if reason == "" {
		return fmt.Sprintf("确认取消订单 %s？", orderID), nil
	}
	return fmt.Sprintf("确认取消订单 %s？取消原因为：%s。", orderID, reason), nil
}

func (b *confirmationSummaryBuilder) orderCreateSummary(ctx context.Context, req ExecuteRequest) (string, error) {
	preOrderID, err := requiredStringArgument(req.Arguments, "pre_order_id")
	if err != nil {
		return "", err
	}
	addressValue, err := requiredInt64Argument(req.Arguments, "address_id")
	if err != nil {
		return "", err
	}
	if _, err := positiveInt32(addressValue, "address_id"); err != nil {
		return "", err
	}
	paymentValue, err := requiredInt64Argument(req.Arguments, "payment_method")
	if err != nil {
		return "", err
	}
	if paymentValue != 1 && paymentValue != 2 {
		return "", invalidArgument("payment_method", "must be 1 or 2")
	}
	if b.checkout == nil {
		return "", ErrToolHandlerRequired
	}
	userID, err := authenticatedUserID32(req.UserID)
	if err != nil {
		return "", err
	}
	resp, err := b.checkout.GetCheckoutDetail(ctx, &checkoutservice.CheckoutDetailReq{PreOrderId: preOrderID, UserId: userID})
	if err != nil {
		return "", fmt.Errorf("checkout_detail before order_create: %w", err)
	}
	if resp == nil || resp.Data == nil {
		return "", fmt.Errorf("checkout_detail before order_create returned empty checkout")
	}
	if err := validateRPCResponse("checkout_detail before order_create", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
		return "", err
	}
	if resp.Data.UserId != int64(userID) {
		return "", fmt.Errorf("checkout_detail before order_create does not belong to authenticated user")
	}
	quantity := int64(0)
	for _, item := range resp.Data.Items {
		if item != nil {
			quantity += int64(item.Quantity)
		}
	}
	summary := fmt.Sprintf("确认使用预订单 %s 创建订单？应付金额 %d 分，商品数量 %d。", preOrderID, resp.Data.FinalAmount, quantity)
	couponID, err := optionalStringArgument(req.Arguments, "coupon_id")
	if err != nil {
		return "", err
	}
	if couponID != "" {
		if b.coupon == nil {
			return "", ErrToolHandlerRequired
		}
		items := make([]*couponsclient.Items, 0, len(resp.Data.Items))
		for _, item := range resp.Data.Items {
			if item != nil {
				items = append(items, &couponsclient.Items{ProductId: item.ProductId, Quantity: item.Quantity})
			}
		}
		calculated, err := b.coupon.CalculateCoupon(ctx, &couponsclient.CalculateCouponReq{
			UserId: userID, CouponId: couponID, Items: items,
		})
		if err != nil {
			return "", fmt.Errorf("coupon_calculate before order_create: %w", err)
		}
		if calculated == nil {
			return "", fmt.Errorf("coupon_calculate before order_create returned nil response")
		}
		if err := validateRPCResponse("coupon_calculate before order_create", calculated, int64(calculated.StatusCode), calculated.StatusMsg); err != nil {
			return "", err
		}
		if !calculated.IsUsable {
			return "", invalidArgument("coupon_id", "is not usable for the checkout items")
		}
		summary = fmt.Sprintf("确认使用预订单 %s 和优惠券 %s 创建订单？应付金额 %d 分，商品数量 %d。", preOrderID, couponID, calculated.FinalAmount, quantity)
	}
	return summary, nil
}

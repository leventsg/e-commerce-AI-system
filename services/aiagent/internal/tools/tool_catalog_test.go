package tools

import (
	"context"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"google.golang.org/grpc"
)

func TestDefaultToolsBuildsCompleteUnifiedCatalog(t *testing.T) {
	tools := DefaultTools(DefaultToolClients{
		Product:   &fakeProductQueryRPC{},
		Inventory: &fakeInventoryQueryRPC{},
		Order:     catalogOrderRPC{},
		Cart:      catalogCartRPC{},
		Coupon:    catalogCouponRPC{},
		Checkout:  catalogCheckoutRPC{},
	}, config.ToolTimeoutConfig{})

	if len(tools) != 20 {
		t.Fatalf("len(DefaultTools) = %d, want 20", len(tools))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" || tool.Desc == "" {
			t.Fatalf("tool has empty name/desc: %#v", tool)
		}
		if tool.Params == nil {
			t.Fatalf("tool %q params is nil", tool.Name)
		}
		if tool.Metadata.Name != tool.Name {
			t.Fatalf("tool %q metadata name = %q", tool.Name, tool.Metadata.Name)
		}
		if tool.Handler == nil {
			t.Fatalf("tool %q handler is nil", tool.Name)
		}
		if seen[tool.Name] {
			t.Fatalf("duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Metadata.RequireConfirmation && tool.ConfirmationSummary == nil {
			t.Fatalf("high-risk tool %q missing confirmation summary", tool.Name)
		}
		if !tool.Metadata.RequireConfirmation && tool.ConfirmationSummary != nil {
			t.Fatalf("non-confirmation tool %q has confirmation summary", tool.Name)
		}
	}
	for _, name := range []string{domain.ToolProductSearch, domain.ToolCartDelete, domain.ToolOrderCreate, domain.ToolOrderCancel} {
		if !seen[name] {
			t.Fatalf("catalog missing %s", name)
		}
	}
}

type catalogCartRPC struct{}

func (catalogCartRPC) CartItemList(ctx context.Context, in *cartsclient.UserInfo, opts ...grpc.CallOption) (*cartsclient.CartItemListResponse, error) {
	return (&fakeCartQueryRPC{}).CartItemList(ctx, in, opts...)
}

func (catalogCartRPC) CreateCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.CreateCartResponse, error) {
	return (&fakeCartWriteRPC{}).CreateCartItem(ctx, in, opts...)
}

func (catalogCartRPC) SubCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.SubCartResponse, error) {
	return (&fakeCartWriteRPC{}).SubCartItem(ctx, in, opts...)
}

func (catalogCartRPC) DeleteCartItem(ctx context.Context, in *cartsclient.CartItemRequest, opts ...grpc.CallOption) (*cartsclient.EmptyCartResponse, error) {
	return (&fakeHighRiskCartRPC{}).DeleteCartItem(ctx, in, opts...)
}

type catalogOrderRPC struct{}

func (catalogOrderRPC) GetOrder(ctx context.Context, in *orderservice.GetOrderRequest, opts ...grpc.CallOption) (*orderservice.OrderDetailResponse, error) {
	return (&fakeOrderQueryRPC{}).GetOrder(ctx, in, opts...)
}

func (catalogOrderRPC) ListOrders(ctx context.Context, in *orderservice.ListOrdersRequest, opts ...grpc.CallOption) (*orderservice.ListOrdersResponse, error) {
	return (&fakeOrderQueryRPC{}).ListOrders(ctx, in, opts...)
}

func (catalogOrderRPC) CreateOrder(ctx context.Context, in *orderservice.CreateOrderRequest, opts ...grpc.CallOption) (*orderservice.OrderDetailResponse, error) {
	return (&fakeHighRiskOrderRPC{}).CreateOrder(ctx, in, opts...)
}

func (catalogOrderRPC) CancelOrder(ctx context.Context, in *orderservice.CancelOrderRequest, opts ...grpc.CallOption) (*orderservice.EmptyRes, error) {
	return (&fakeHighRiskOrderRPC{}).CancelOrder(ctx, in, opts...)
}

type catalogCouponRPC struct{}

func (catalogCouponRPC) ListCoupons(ctx context.Context, in *couponsclient.ListCouponsReq, opts ...grpc.CallOption) (*couponsclient.ListCouponsResp, error) {
	return (&fakeCouponQueryRPC{}).ListCoupons(ctx, in, opts...)
}

func (catalogCouponRPC) GetCoupon(ctx context.Context, in *couponsclient.GetCouponReq, opts ...grpc.CallOption) (*couponsclient.GetCouponResp, error) {
	return (&fakeCouponQueryRPC{}).GetCoupon(ctx, in, opts...)
}

func (catalogCouponRPC) ListUserCoupons(ctx context.Context, in *couponsclient.ListUserCouponsReq, opts ...grpc.CallOption) (*couponsclient.ListUserCouponsResp, error) {
	return (&fakeCouponQueryRPC{}).ListUserCoupons(ctx, in, opts...)
}

func (catalogCouponRPC) ListCouponUsages(ctx context.Context, in *couponsclient.ListCouponUsagesReq, opts ...grpc.CallOption) (*couponsclient.ListCouponUsagesResp, error) {
	return (&fakeCouponQueryRPC{}).ListCouponUsages(ctx, in, opts...)
}

func (catalogCouponRPC) CalculateCoupon(ctx context.Context, in *couponsclient.CalculateCouponReq, opts ...grpc.CallOption) (*couponsclient.CalculateCouponResp, error) {
	return (&fakeCouponQueryRPC{}).CalculateCoupon(ctx, in, opts...)
}

func (catalogCouponRPC) ClaimCoupon(ctx context.Context, in *couponsclient.ClaimCouponReq, opts ...grpc.CallOption) (*couponsclient.ClaimCouponResp, error) {
	return (&fakeCouponWriteRPC{}).ClaimCoupon(ctx, in, opts...)
}

type catalogCheckoutRPC struct{}

func (catalogCheckoutRPC) GetCheckoutDetail(ctx context.Context, in *checkoutservice.CheckoutDetailReq, opts ...grpc.CallOption) (*checkoutservice.CheckoutDetailResp, error) {
	return (&fakeCheckoutQueryRPC{}).GetCheckoutDetail(ctx, in, opts...)
}

func (catalogCheckoutRPC) PrepareCheckout(ctx context.Context, in *checkoutservice.CheckoutReq, opts ...grpc.CallOption) (*checkoutservice.CheckoutResp, error) {
	return (&fakeCheckoutWriteRPC{}).PrepareCheckout(ctx, in, opts...)
}

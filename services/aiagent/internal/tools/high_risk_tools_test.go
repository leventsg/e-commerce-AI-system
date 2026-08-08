package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"google.golang.org/grpc"
)

func TestHighRiskEinoToolInvokableRunExecutesAfterApprovalWithoutCreatingConfirmation(t *testing.T) {
	creator := &fakeConfirmationCreator{confirmation: &domain.Confirmation{
		ID: "confirm-1", ToolName: domain.ToolCartDelete, Summary: "确认删除购物车条目 8？",
		ExpiresAt: time.Unix(12345, 0), Arguments: map[string]any{"cart_item_id": float64(8)},
	}}
	cartRPC := &fakeHighRiskCartRPC{
		listResp:   &cartsclient.CartItemListResponse{Data: []*cartsclient.CartInfoResponse{{Id: 8, UserId: 42, ProductId: 11, Quantity: 2}}},
		deleteResp: &cartsclient.EmptyCartResponse{},
	}
	registry := newTestRegistry(DefaultToolClients{CartHighRisk: cartRPC}, config.ToolTimeoutConfig{})
	NewExecutor(registry)
	tool, err := registry.Tool(domain.ToolCartDelete)
	if err != nil {
		t.Fatalf("cart_delete tool: %v", err)
	}
	ctx := WithToolExecutionContext(context.Background(), ToolExecutionContext{
		UserID: 42, ConversationID: "conv-1", MessageID: "msg-1",
	})

	raw, err := tool.InvokableRun(ctx, `{"cart_item_id":8,"user_id":999}`)
	if err != nil {
		t.Fatalf("cart_delete execute: %v", err)
	}
	if cartRPC.deleteCalls != 1 || cartRPC.listCalls != 1 {
		t.Fatalf("business RPC calls: list=%d delete=%d", cartRPC.listCalls, cartRPC.deleteCalls)
	}
	if creator.calls != 0 {
		t.Fatalf("direct invokable should not create confirmation, calls=%d", creator.calls)
	}
	if !strings.Contains(raw, "cart_item_id") {
		t.Fatalf("execution payload = %s", raw)
	}
}

func TestHighRiskCartDeleteSummaryUsesProductName(t *testing.T) {
	creator := &fakeConfirmationCreator{confirmation: &domain.Confirmation{
		ID: "confirm-1", ExpiresAt: time.Unix(12345, 0),
	}}
	cartRPC := &fakeHighRiskCartRPC{listResp: &cartsclient.CartItemListResponse{
		Data: []*cartsclient.CartInfoResponse{{Id: 3, UserId: 42, ProductId: 11, Quantity: 1}},
	}}
	productRPC := &fakeProductQueryRPC{detailResp: &productcatalogservice.GetProductResp{
		Product: &productcatalogservice.Product{Id: 11, Name: "无线蓝牙耳机"},
	}}
	approval := newTestApprovalManager(DefaultToolClients{CartHighRisk: cartRPC, Product: productRPC}, creator)

	event := approval.RequestConfirmation(context.Background(), ExecuteRequest{
		UserID:         42,
		ConversationID: "conv-1",
		ToolName:       domain.ToolCartDelete,
		Arguments:      map[string]any{"cart_item_id": 3},
	})

	if event.Type != domain.EventConfirmationRequired {
		t.Fatalf("event = %#v", event)
	}
	if !strings.Contains(event.Summary, "无线蓝牙耳机") || !strings.Contains(event.Summary, "数量 1") {
		t.Fatalf("summary = %q, want user-friendly product name and quantity", event.Summary)
	}
	if strings.Contains(event.Summary, "条目 3") {
		t.Fatalf("summary = %q, should not expose cart item id as the primary label", event.Summary)
	}
	if productRPC.detailReq == nil || productRPC.detailReq.Id != 11 || productRPC.detailReq.UserId != 42 {
		t.Fatalf("GetProduct request = %#v", productRPC.detailReq)
	}
}

func TestHighRiskOrderCreateSummaryUsesOwnedCheckoutDetails(t *testing.T) {
	creator := &fakeConfirmationCreator{confirmation: &domain.Confirmation{ID: "confirm-2", ExpiresAt: time.Unix(12345, 0)}}
	checkoutRPC := &fakeHighRiskCheckoutRPC{resp: &checkoutservice.CheckoutDetailResp{
		Data: &checkout.CheckoutOrder{
			PreOrderId: "pre-1", UserId: 42, FinalAmount: 8800,
			Items: []*checkout.CheckoutItem{{ProductId: 11, Quantity: 2}, {ProductId: 12, Quantity: 1}},
		},
	}}
	couponRPC := &fakeCouponCalculateRPC{resp: &couponsclient.CalculateCouponResp{
		IsUsable: true, FinalAmount: 7600, DiscountAmount: 1200,
	}}
	approval := newTestApprovalManager(DefaultToolClients{CheckoutQuery: checkoutRPC, CouponCalculate: couponRPC}, creator)

	event := approval.RequestConfirmation(context.Background(), ExecuteRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCreate,
		Arguments: map[string]any{
			"pre_order_id": "pre-1", "address_id": 7, "payment_method": 1,
			"coupon_id": "coupon-1", "user_id": 999,
		},
	})

	if event.Type != domain.EventConfirmationRequired || event.Status != confirmation.StatusPending {
		t.Fatalf("confirmation event = %#v", event)
	}
	if checkoutRPC.req == nil || checkoutRPC.req.UserId != 42 || checkoutRPC.req.PreOrderId != "pre-1" {
		t.Fatalf("GetCheckoutDetail request = %#v", checkoutRPC.req)
	}
	for _, part := range []string{"pre-1", "7600", "3", "coupon-1"} {
		if !strings.Contains(creator.req.Summary, part) {
			t.Fatalf("summary %q missing %q", creator.req.Summary, part)
		}
	}
	if _, ok := creator.req.Arguments["user_id"]; ok {
		t.Fatalf("order confirmation persisted user_id: %#v", creator.req.Arguments)
	}
	if couponRPC.req == nil || couponRPC.req.UserId != 42 || couponRPC.req.CouponId != "coupon-1" || len(couponRPC.req.Items) != 2 {
		t.Fatalf("CalculateCoupon request = %#v", couponRPC.req)
	}
}

func TestHighRiskOrderCreateDoesNotCreateConfirmationForInvalidCheckout(t *testing.T) {
	creator := &fakeConfirmationCreator{}
	approval := newTestApprovalManager(DefaultToolClients{
		CheckoutQuery: &fakeHighRiskCheckoutRPC{err: errors.New("not owned")},
	}, creator)

	event := approval.RequestConfirmation(context.Background(), ExecuteRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCreate,
		Arguments: map[string]any{"pre_order_id": "pre-1", "address_id": 7, "payment_method": 1},
	})

	if event.Status != toolStatusFailed || creator.calls != 0 {
		t.Fatalf("event=%#v creator calls=%d", event, creator.calls)
	}
}

func TestHighRiskOrderCreateRejectsCheckoutOwnedByAnotherUser(t *testing.T) {
	creator := &fakeConfirmationCreator{}
	approval := newTestApprovalManager(DefaultToolClients{
		CheckoutQuery: &fakeHighRiskCheckoutRPC{resp: &checkoutservice.CheckoutDetailResp{Data: &checkout.CheckoutOrder{
			PreOrderId: "pre-1", UserId: 99, FinalAmount: 100,
		}}},
	}, creator)

	event := approval.RequestConfirmation(context.Background(), ExecuteRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCreate,
		Arguments: map[string]any{"pre_order_id": "pre-1", "address_id": 7, "payment_method": 1},
	})

	if event.Status != toolStatusFailed || creator.calls != 0 {
		t.Fatalf("event=%#v creator calls=%d", event, creator.calls)
	}
}

func TestHighRiskConfirmationCreationRecordsToolCallWithoutWriteAuditMetadata(t *testing.T) {
	recorder := &capturingToolCallRecorder{}
	creator := &fakeConfirmationCreator{confirmation: &domain.Confirmation{
		ID: "confirm-1", ExpiresAt: time.Unix(12345, 0),
	}}
	approval := newTestApprovalManager(DefaultToolClients{}, creator, WithToolCallRecorder(recorder))

	event := approval.RequestConfirmation(context.Background(), ExecuteRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCancel,
		Arguments: map[string]any{"order_id": "order-1"},
	})

	if event.Type != domain.EventConfirmationRequired || len(recorder.records) != 1 {
		t.Fatalf("event=%#v records=%#v", event, recorder.records)
	}
	record := recorder.records[0]
	if record.Status != toolStatusSuccess || record.Metadata.WriteOperation {
		t.Fatalf("confirmation record = %#v", record)
	}
}

func TestHighRiskConfirmedHandlersExecuteTrustedRPCRequests(t *testing.T) {
	cartRPC := &fakeHighRiskCartRPC{listResp: &cartsclient.CartItemListResponse{
		Data: []*cartsclient.CartInfoResponse{{Id: 8, UserId: 42, ProductId: 11, Quantity: 2}},
	}, deleteResp: &cartsclient.EmptyCartResponse{}}
	orderRPC := &fakeHighRiskOrderRPC{
		createResp: &orderservice.OrderDetailResponse{Order: &orderservice.Order{OrderId: "order-1", PreOrderId: "pre-1", PayableAmount: 8800}},
		cancelResp: &orderservice.EmptyRes{},
	}
	harness := newTestToolHarness(DefaultToolClients{CartHighRisk: cartRPC, OrderHighRisk: orderRPC})

	deleteEvent := harness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartDelete, Arguments: map[string]any{"cart_item_id": 8, "user_id": 999},
	})
	assertWriteToolSuccess(t, deleteEvent, domain.ToolCartDelete)
	if cartRPC.deleteReq == nil || cartRPC.deleteReq.UserId != 42 || cartRPC.deleteReq.ProductId != 11 {
		t.Fatalf("DeleteCartItem request = %#v", cartRPC.deleteReq)
	}

	createEvent := harness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolOrderCreate,
		Arguments: map[string]any{"pre_order_id": "pre-1", "address_id": 7, "payment_method": 2, "coupon_id": "coupon-1", "user_id": 999},
	})
	assertWriteToolSuccess(t, createEvent, domain.ToolOrderCreate)
	if orderRPC.createReq == nil || orderRPC.createReq.UserId != 42 || orderRPC.createReq.AddressId != 7 || orderRPC.createReq.PaymentMethod != order.PaymentMethod_ALIPAY {
		t.Fatalf("CreateOrder request = %#v", orderRPC.createReq)
	}

	cancelEvent := harness.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolOrderCancel,
		Arguments: map[string]any{"order_id": "order-1", "reason": "不需要了", "user_id": 999},
	})
	assertWriteToolSuccess(t, cancelEvent, domain.ToolOrderCancel)
	if orderRPC.cancelReq == nil || orderRPC.cancelReq.UserId != 42 || !orderRPC.cancelReq.Initiative || orderRPC.cancelReq.CancelReason != "不需要了" {
		t.Fatalf("CancelOrder request = %#v", orderRPC.cancelReq)
	}
}

type fakeConfirmationCreator struct {
	req          confirmation.CreateRequest
	confirmation *domain.Confirmation
	err          error
	calls        int
}

func (f *fakeConfirmationCreator) Create(_ context.Context, req confirmation.CreateRequest) (*domain.Confirmation, error) {
	f.calls++
	f.req = req
	if f.confirmation != nil {
		f.confirmation.ToolName = req.ToolName
		f.confirmation.Arguments = req.Arguments
		f.confirmation.Summary = req.Summary
	}
	return f.confirmation, f.err
}

type fakeHighRiskCartRPC struct {
	listCalls   int
	deleteCalls int
	listResp    *cartsclient.CartItemListResponse
	listErr     error
	deleteReq   *cartsclient.CartItemRequest
	deleteResp  *cartsclient.EmptyCartResponse
	deleteErr   error
}

func (f *fakeHighRiskCartRPC) CartItemList(_ context.Context, _ *cartsclient.UserInfo, _ ...grpc.CallOption) (*cartsclient.CartItemListResponse, error) {
	f.listCalls++
	return f.listResp, f.listErr
}

func (f *fakeHighRiskCartRPC) DeleteCartItem(_ context.Context, req *cartsclient.CartItemRequest, _ ...grpc.CallOption) (*cartsclient.EmptyCartResponse, error) {
	f.deleteCalls++
	f.deleteReq = req
	return f.deleteResp, f.deleteErr
}

type fakeHighRiskCheckoutRPC struct {
	req  *checkoutservice.CheckoutDetailReq
	resp *checkoutservice.CheckoutDetailResp
	err  error
}

func (f *fakeHighRiskCheckoutRPC) GetCheckoutDetail(_ context.Context, req *checkoutservice.CheckoutDetailReq, _ ...grpc.CallOption) (*checkoutservice.CheckoutDetailResp, error) {
	f.req = req
	return f.resp, f.err
}

type fakeHighRiskOrderRPC struct {
	createReq  *orderservice.CreateOrderRequest
	createResp *orderservice.OrderDetailResponse
	createErr  error
	cancelReq  *orderservice.CancelOrderRequest
	cancelResp *orderservice.EmptyRes
	cancelErr  error
}

type fakeCouponCalculateRPC struct {
	req  *couponsclient.CalculateCouponReq
	resp *couponsclient.CalculateCouponResp
	err  error
}

func (f *fakeCouponCalculateRPC) CalculateCoupon(_ context.Context, req *couponsclient.CalculateCouponReq, _ ...grpc.CallOption) (*couponsclient.CalculateCouponResp, error) {
	f.req = req
	return f.resp, f.err
}

func (f *fakeHighRiskOrderRPC) CreateOrder(_ context.Context, req *orderservice.CreateOrderRequest, _ ...grpc.CallOption) (*orderservice.OrderDetailResponse, error) {
	f.createReq = req
	return f.createResp, f.createErr
}

func (f *fakeHighRiskOrderRPC) CancelOrder(_ context.Context, req *orderservice.CancelOrderRequest, _ ...grpc.CallOption) (*orderservice.EmptyRes, error) {
	f.cancelReq = req
	return f.cancelResp, f.cancelErr
}

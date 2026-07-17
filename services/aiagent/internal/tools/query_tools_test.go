package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"google.golang.org/grpc"
)

func TestQueryToolsRegistersAllQueryHandlers(t *testing.T) {
	queryTools := newTestQueryTools(QueryToolClients{
		Product:   &fakeProductQueryRPC{},
		Inventory: &fakeInventoryQueryRPC{},
		Order:     &fakeOrderQueryRPC{},
		Cart:      &fakeCartQueryRPC{},
		Coupon:    &fakeCouponQueryRPC{},
		Checkout:  &fakeCheckoutQueryRPC{},
	})
	want := []string{
		domain.ToolProductSearch,
		domain.ToolProductDetail,
		domain.ToolProductRecommend,
		domain.ToolInventoryGet,
		domain.ToolOrderGet,
		domain.ToolOrderList,
		domain.ToolCartList,
		domain.ToolCouponList,
		domain.ToolCouponDetail,
		domain.ToolCouponMyList,
		domain.ToolCouponUsageList,
		domain.ToolCouponCalculate,
		domain.ToolCheckoutDetail,
	}
	for _, name := range want {
		if _, ok := queryTools.Handler(name); !ok {
			t.Fatalf("query handler %q was not registered", name)
		}
	}
}

func TestQueryToolsRegistrySchemasMatchRPCContracts(t *testing.T) {
	registry := NewRegistry(config.ToolTimeoutConfig{})

	inventoryTool, err := registry.Tool(domain.ToolInventoryGet)
	if err != nil {
		t.Fatalf("inventory tool: %v", err)
	}
	inventoryInfo, err := inventoryTool.Info(context.Background())
	if err != nil {
		t.Fatalf("inventory info: %v", err)
	}
	inventoryJSON, err := json.Marshal(inventoryInfo)
	if err != nil {
		t.Fatalf("marshal inventory schema: %v", err)
	}
	if !strings.Contains(string(inventoryJSON), "product_id") || strings.Contains(string(inventoryJSON), "sku_id") {
		t.Fatalf("inventory schema does not match GetInventory RPC: %s", inventoryJSON)
	}

	couponTool, err := registry.Tool(domain.ToolCouponCalculate)
	if err != nil {
		t.Fatalf("coupon calculate tool: %v", err)
	}
	couponInfo, err := couponTool.Info(context.Background())
	if err != nil {
		t.Fatalf("coupon calculate info: %v", err)
	}
	couponJSON, err := json.Marshal(couponInfo)
	if err != nil {
		t.Fatalf("marshal coupon schema: %v", err)
	}
	if !strings.Contains(string(couponJSON), "items") || strings.Contains(string(couponJSON), `"amount"`) {
		t.Fatalf("coupon.calculate schema does not match CalculateCoupon RPC: %s", couponJSON)
	}
}

func TestQueryToolsEinoHandlerUsesTrustedExecutionContext(t *testing.T) {
	productRPC := &fakeProductQueryRPC{detailResp: &productcatalogservice.GetProductResp{
		Product: &productcatalogservice.Product{Id: 12, Name: "学生手机", Price: 99900},
	}}
	registry := NewRegistry(config.ToolTimeoutConfig{})
	NewQueryTools(NewExecutor(registry), QueryToolClients{Product: productRPC})
	tool, err := registry.Tool(domain.ToolProductDetail)
	if err != nil {
		t.Fatalf("get product detail tool: %v", err)
	}
	ctx := WithToolExecutionContext(context.Background(), ToolExecutionContext{
		UserID:         42,
		ConversationID: "conv-1",
		MessageID:      "msg-1",
	})
	raw, err := tool.InvokableRun(ctx, `{"product_id":12,"user_id":999}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if productRPC.detailReq == nil || productRPC.detailReq.UserId != 42 {
		t.Fatalf("GetProduct request = %#v, want trusted user 42", productRPC.detailReq)
	}
	if !strings.Contains(raw, `"product_id":12`) {
		t.Fatalf("tool result = %s, want compact product payload", raw)
	}
}

func TestQueryToolsEinoHandlerRejectsMissingTrustedExecutionContext(t *testing.T) {
	productRPC := &fakeProductQueryRPC{detailResp: &productcatalogservice.GetProductResp{
		Product: &productcatalogservice.Product{Id: 12, Name: "学生手机"},
	}}
	registry := NewRegistry(config.ToolTimeoutConfig{})
	NewQueryTools(NewExecutor(registry), QueryToolClients{Product: productRPC})
	tool, err := registry.Tool(domain.ToolProductDetail)
	if err != nil {
		t.Fatalf("get product detail tool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"product_id":12,"user_id":999}`); !errors.Is(err, ErrToolExecutionContext) {
		t.Fatalf("InvokableRun error = %v, want ErrToolExecutionContext", err)
	}
	if productRPC.detailReq != nil {
		t.Fatalf("GetProduct was called without trusted context: %#v", productRPC.detailReq)
	}
}

func TestQueryToolsProductHandlersConvertArgumentsAndInjectUser(t *testing.T) {
	productRPC := &fakeProductQueryRPC{
		queryResp: &productcatalogservice.GetAllProductsResp{
			Total: 1,
			Products: []*productcatalogservice.Product{{
				Id: 12, Name: "学生手机", Price: 99900, Stock: 8, Categories: []string{"手机"},
			}},
		},
		detailResp: &productcatalogservice.GetProductResp{
			Product: &productcatalogservice.Product{Id: 12, Name: "学生手机", Price: 99900},
		},
		recommendResp: &productcatalogservice.GetAllProductsResp{
			Products: []*productcatalogservice.Product{{Id: 13, Name: "推荐手机", Price: 129900}},
		},
	}
	queryTools := newTestQueryTools(QueryToolClients{Product: productRPC})

	searchEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID:   42,
		ToolName: domain.ToolProductSearch,
		Arguments: map[string]any{
			"keyword":   "学生",
			"category":  "手机",
			"min_price": 50000,
			"max_price": 150000,
			"page":      2,
			"page_size": 5,
			"user_id":   999,
		},
	})
	assertQueryToolSuccess(t, searchEvent, domain.ToolProductSearch)
	if productRPC.queryReq.Keyword != "学生" || len(productRPC.queryReq.Category) != 1 || productRPC.queryReq.Category[0] != "手机" {
		t.Fatalf("QueryProduct request filters = %#v", productRPC.queryReq)
	}
	if productRPC.queryReq.Price.Min != 50000 || productRPC.queryReq.Price.Max != 150000 {
		t.Fatalf("QueryProduct price = %#v", productRPC.queryReq.Price)
	}
	if productRPC.queryReq.Paginator.Page != 2 || productRPC.queryReq.Paginator.PageSize != 5 {
		t.Fatalf("QueryProduct paginator = %#v", productRPC.queryReq.Paginator)
	}
	searchData := decodeEventData(t, searchEvent)
	if searchData["total"] != float64(1) {
		t.Fatalf("search total = %#v, want 1", searchData["total"])
	}

	detailEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID:    42,
		ToolName:  domain.ToolProductDetail,
		Arguments: map[string]any{"product_id": 12, "user_id": 999},
	})
	assertQueryToolSuccess(t, detailEvent, domain.ToolProductDetail)
	if productRPC.detailReq.Id != 12 || productRPC.detailReq.UserId != 42 {
		t.Fatalf("GetProduct request = %#v, want product 12 user 42", productRPC.detailReq)
	}

	recommendEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID:   42,
		ToolName: domain.ToolProductRecommend,
		Arguments: map[string]any{
			"query":    "学生手机",
			"category": "手机",
			"limit":    3,
			"user_id":  999,
		},
	})
	assertQueryToolSuccess(t, recommendEvent, domain.ToolProductRecommend)
	if productRPC.recommendReq.UserId != 42 || productRPC.recommendReq.Paginator.PageSize != 3 {
		t.Fatalf("RecommendProduct request = %#v, want user 42 limit 3", productRPC.recommendReq)
	}
}

func TestQueryToolsInventoryAndOrderHandlersUseAuthenticatedUser(t *testing.T) {
	inventoryRPC := &fakeInventoryQueryRPC{resp: &inventoryclient.GetInventoryResp{Inventory: 9, SoldCount: 4}}
	orderRPC := &fakeOrderQueryRPC{
		getResp: &orderservice.OrderDetailResponse{
			Order: &orderservice.Order{OrderId: "order-1", UserId: 42, OrderStatus: order.OrderStatus_ORDER_STATUS_PAID},
			Items: []*orderservice.OrderItem{{ProductId: 12, Quantity: 2, ProductName: "手机", UnitPrice: 99900}},
		},
		listResp: &orderservice.ListOrdersResponse{
			Orders: []*orderservice.Order{{OrderId: "order-1", UserId: 42, OrderStatus: order.OrderStatus_ORDER_STATUS_PAID}},
		},
	}
	queryTools := newTestQueryTools(QueryToolClients{Inventory: inventoryRPC, Order: orderRPC})

	inventoryEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolInventoryGet, Arguments: map[string]any{"product_id": 12},
	})
	assertQueryToolSuccess(t, inventoryEvent, domain.ToolInventoryGet)
	if inventoryRPC.req.ProductId != 12 {
		t.Fatalf("GetInventory product = %d, want 12", inventoryRPC.req.ProductId)
	}

	getEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolOrderGet, Arguments: map[string]any{"order_id": "order-1", "user_id": 999},
	})
	assertQueryToolSuccess(t, getEvent, domain.ToolOrderGet)
	if orderRPC.getReq.UserId != 42 || orderRPC.getReq.OrderId != "order-1" {
		t.Fatalf("GetOrder request = %#v", orderRPC.getReq)
	}

	listEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID:    42,
		ToolName:  domain.ToolOrderList,
		Arguments: map[string]any{"page": 2, "page_size": 7, "status": "paid", "user_id": 999},
	})
	assertQueryToolSuccess(t, listEvent, domain.ToolOrderList)
	if orderRPC.listReq.UserId != 42 || orderRPC.listReq.Pagination.Page != 2 || orderRPC.listReq.Pagination.PageSize != 7 {
		t.Fatalf("ListOrders request = %#v", orderRPC.listReq)
	}
	wantStatus := order.OrderStatus_ORDER_STATUS_PAID
	if len(orderRPC.listReq.StatusFilter.Statuses) != 1 || orderRPC.listReq.StatusFilter.Statuses[0] != wantStatus {
		t.Fatalf("ListOrders statuses = %#v, want PAID", orderRPC.listReq.StatusFilter.Statuses)
	}
}

func TestQueryToolsCartCouponAndCheckoutHandlersConvertArguments(t *testing.T) {
	cartRPC := &fakeCartQueryRPC{resp: &cartsclient.CartItemListResponse{
		Total: 3,
		Data: []*cartsclient.CartInfoResponse{
			{Id: 1, ProductId: 11, Quantity: 1},
			{Id: 2, ProductId: 12, Quantity: 2},
			{Id: 3, ProductId: 13, Quantity: 3},
		},
	}}
	couponRPC := &fakeCouponQueryRPC{
		listResp: &couponsclient.ListCouponsResp{TotalCount: 1, Coupons: []*couponsclient.Coupon{{Id: "coupon-1", Name: "满减券"}}},
		getResp:  &couponsclient.GetCouponResp{Coupon: &couponsclient.Coupon{Id: "coupon-1", Name: "满减券"}},
		userResp: &couponsclient.ListUserCouponsResp{TotalCount: 2, UserCoupons: []*couponsclient.UserCoupon{
			{Id: 1, UserId: 42, CouponId: "coupon-1", Status: coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED},
			{Id: 2, UserId: 42, CouponId: "coupon-2", Status: coupons.CouponStatus_COUPON_STATUS_USED},
		}},
		usageResp: &couponsclient.ListCouponUsagesResp{TotalCount: 1, Usages: []*couponsclient.CouponUsage{{Id: 1, CouponId: "coupon-1", UserId: 42}}},
		calculateResp: &couponsclient.CalculateCouponResp{
			OriginAmount: 20000, FinalAmount: 18000, DiscountAmount: 2000, IsUsable: true,
		},
	}
	checkoutRPC := &fakeCheckoutQueryRPC{resp: &checkoutservice.CheckoutDetailResp{Data: &checkoutservice.CheckoutOrder{
		PreOrderId: "pre-1", UserId: 42, OriginalAmount: 20000, FinalAmount: 18000,
	}}}
	queryTools := newTestQueryTools(QueryToolClients{Cart: cartRPC, Coupon: couponRPC, Checkout: checkoutRPC})

	cartEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCartList, Arguments: map[string]any{"page": 2, "page_size": 1, "user_id": 999},
	})
	assertQueryToolSuccess(t, cartEvent, domain.ToolCartList)
	if cartRPC.req.Id != 42 {
		t.Fatalf("CartItemList user = %d, want 42", cartRPC.req.Id)
	}
	cartData := decodeEventData(t, cartEvent)
	items := cartData["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["cart_item_id"] != float64(2) {
		t.Fatalf("paginated cart items = %#v", items)
	}

	assertQueryToolSuccess(t, queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponList, Arguments: map[string]any{"page": 2, "page_size": 4},
	}), domain.ToolCouponList)
	if couponRPC.listReq.Pagination.Page != 2 || couponRPC.listReq.Pagination.Size != 4 {
		t.Fatalf("ListCoupons pagination = %#v", couponRPC.listReq.Pagination)
	}

	assertQueryToolSuccess(t, queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponDetail, Arguments: map[string]any{"coupon_id": "coupon-1"},
	}), domain.ToolCouponDetail)
	if couponRPC.getReq.Id != "coupon-1" {
		t.Fatalf("GetCoupon id = %q", couponRPC.getReq.Id)
	}

	myEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponMyList,
		Arguments: map[string]any{"page": 1, "page_size": 10, "status": "used", "user_id": 999},
	})
	assertQueryToolSuccess(t, myEvent, domain.ToolCouponMyList)
	if couponRPC.userReq.UserId != 42 {
		t.Fatalf("ListUserCoupons user = %d, want 42", couponRPC.userReq.UserId)
	}
	myData := decodeEventData(t, myEvent)
	if len(myData["coupons"].([]any)) != 1 {
		t.Fatalf("filtered user coupons = %#v", myData["coupons"])
	}

	assertQueryToolSuccess(t, queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponUsageList,
		Arguments: map[string]any{"page": 1, "page_size": 10, "user_id": 999},
	}), domain.ToolCouponUsageList)
	if couponRPC.usageReq.UserId != 42 {
		t.Fatalf("ListCouponUsages user = %d, want 42", couponRPC.usageReq.UserId)
	}

	calculateEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCouponCalculate,
		Arguments: map[string]any{
			"coupon_id": "coupon-1",
			"items":     []any{map[string]any{"product_id": 12, "quantity": 2}},
			"user_id":   999,
		},
	})
	assertQueryToolSuccess(t, calculateEvent, domain.ToolCouponCalculate)
	if couponRPC.calculateReq.UserId != 42 || len(couponRPC.calculateReq.Items) != 1 || couponRPC.calculateReq.Items[0].ProductId != 12 {
		t.Fatalf("CalculateCoupon request = %#v", couponRPC.calculateReq)
	}

	checkoutEvent := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolCheckoutDetail,
		Arguments: map[string]any{"pre_order_id": "pre-1", "user_id": 999},
	})
	assertQueryToolSuccess(t, checkoutEvent, domain.ToolCheckoutDetail)
	if checkoutRPC.req.UserId != 42 || checkoutRPC.req.PreOrderId != "pre-1" {
		t.Fatalf("GetCheckoutDetail request = %#v", checkoutRPC.req)
	}
}

func TestQueryToolsBusinessFailureReturnsFailedEvent(t *testing.T) {
	productRPC := &fakeProductQueryRPC{detailResp: &productcatalogservice.GetProductResp{
		StatusCode: 60001,
		StatusMsg:  "商品不存在",
	}}
	queryTools := newTestQueryTools(QueryToolClients{Product: productRPC})

	event := queryTools.Execute(context.Background(), ExecuteRequest{
		UserID: 42, ToolName: domain.ToolProductDetail, Arguments: map[string]any{"product_id": 404},
	})
	if event.Status != toolStatusFailed {
		t.Fatalf("event.Status = %q, want failed", event.Status)
	}
}

func newTestQueryTools(clients QueryToolClients) *QueryTools {
	return NewQueryTools(NewExecutor(NewRegistry(config.ToolTimeoutConfig{})), clients)
}

func assertQueryToolSuccess(t *testing.T, event domain.AgentEvent, toolName string) {
	t.Helper()
	if event.Type != domain.EventToolResult || event.Tool != toolName || event.Status != toolStatusSuccess {
		t.Fatalf("event = %#v, want successful %s tool result", event, toolName)
	}
}

func decodeEventData(t *testing.T, event domain.AgentEvent) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal([]byte(event.DataJSON), &data); err != nil {
		t.Fatalf("unmarshal event data %q: %v", event.DataJSON, err)
	}
	return data
}

type fakeProductQueryRPC struct {
	queryReq      *productcatalogservice.QueryProductReq
	detailReq     *productcatalogservice.GetProductReq
	recommendReq  *productcatalogservice.RecommendProductReq
	queryResp     *productcatalogservice.GetAllProductsResp
	detailResp    *productcatalogservice.GetProductResp
	recommendResp *productcatalogservice.GetAllProductsResp
}

func (f *fakeProductQueryRPC) QueryProduct(_ context.Context, req *productcatalogservice.QueryProductReq, _ ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error) {
	f.queryReq = req
	return f.queryResp, nil
}

func (f *fakeProductQueryRPC) GetProduct(_ context.Context, req *productcatalogservice.GetProductReq, _ ...grpc.CallOption) (*productcatalogservice.GetProductResp, error) {
	f.detailReq = req
	return f.detailResp, nil
}

func (f *fakeProductQueryRPC) RecommendProduct(_ context.Context, req *productcatalogservice.RecommendProductReq, _ ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error) {
	f.recommendReq = req
	return f.recommendResp, nil
}

type fakeInventoryQueryRPC struct {
	req  *inventoryclient.GetInventoryReq
	resp *inventoryclient.GetInventoryResp
}

func (f *fakeInventoryQueryRPC) GetInventory(_ context.Context, req *inventoryclient.GetInventoryReq, _ ...grpc.CallOption) (*inventoryclient.GetInventoryResp, error) {
	f.req = req
	return f.resp, nil
}

type fakeOrderQueryRPC struct {
	getReq   *orderservice.GetOrderRequest
	listReq  *orderservice.ListOrdersRequest
	getResp  *orderservice.OrderDetailResponse
	listResp *orderservice.ListOrdersResponse
}

func (f *fakeOrderQueryRPC) GetOrder(_ context.Context, req *orderservice.GetOrderRequest, _ ...grpc.CallOption) (*orderservice.OrderDetailResponse, error) {
	f.getReq = req
	return f.getResp, nil
}

func (f *fakeOrderQueryRPC) ListOrders(_ context.Context, req *orderservice.ListOrdersRequest, _ ...grpc.CallOption) (*orderservice.ListOrdersResponse, error) {
	f.listReq = req
	return f.listResp, nil
}

type fakeCartQueryRPC struct {
	req  *cartsclient.UserInfo
	resp *cartsclient.CartItemListResponse
}

func (f *fakeCartQueryRPC) CartItemList(_ context.Context, req *cartsclient.UserInfo, _ ...grpc.CallOption) (*cartsclient.CartItemListResponse, error) {
	f.req = req
	return f.resp, nil
}

type fakeCouponQueryRPC struct {
	listReq       *couponsclient.ListCouponsReq
	getReq        *couponsclient.GetCouponReq
	userReq       *couponsclient.ListUserCouponsReq
	usageReq      *couponsclient.ListCouponUsagesReq
	calculateReq  *couponsclient.CalculateCouponReq
	listResp      *couponsclient.ListCouponsResp
	getResp       *couponsclient.GetCouponResp
	userResp      *couponsclient.ListUserCouponsResp
	usageResp     *couponsclient.ListCouponUsagesResp
	calculateResp *couponsclient.CalculateCouponResp
}

func (f *fakeCouponQueryRPC) ListCoupons(_ context.Context, req *couponsclient.ListCouponsReq, _ ...grpc.CallOption) (*couponsclient.ListCouponsResp, error) {
	f.listReq = req
	return f.listResp, nil
}

func (f *fakeCouponQueryRPC) GetCoupon(_ context.Context, req *couponsclient.GetCouponReq, _ ...grpc.CallOption) (*couponsclient.GetCouponResp, error) {
	f.getReq = req
	return f.getResp, nil
}

func (f *fakeCouponQueryRPC) ListUserCoupons(_ context.Context, req *couponsclient.ListUserCouponsReq, _ ...grpc.CallOption) (*couponsclient.ListUserCouponsResp, error) {
	f.userReq = req
	return f.userResp, nil
}

func (f *fakeCouponQueryRPC) ListCouponUsages(_ context.Context, req *couponsclient.ListCouponUsagesReq, _ ...grpc.CallOption) (*couponsclient.ListCouponUsagesResp, error) {
	f.usageReq = req
	return f.usageResp, nil
}

func (f *fakeCouponQueryRPC) CalculateCoupon(_ context.Context, req *couponsclient.CalculateCouponReq, _ ...grpc.CallOption) (*couponsclient.CalculateCouponResp, error) {
	f.calculateReq = req
	return f.calculateResp, nil
}

type fakeCheckoutQueryRPC struct {
	req  *checkoutservice.CheckoutDetailReq
	resp *checkoutservice.CheckoutDetailResp
}

func (f *fakeCheckoutQueryRPC) GetCheckoutDetail(_ context.Context, req *checkoutservice.CheckoutDetailReq, _ ...grpc.CallOption) (*checkoutservice.CheckoutDetailResp, error) {
	f.req = req
	return f.resp, nil
}

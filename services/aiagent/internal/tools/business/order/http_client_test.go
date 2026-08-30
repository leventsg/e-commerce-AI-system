package order_tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

func TestOrderAPIClientCreateOrder(t *testing.T) {
	var gotClientIP string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/douyin/order/internal/create" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get(biz.TokenKey); got != "" {
			t.Fatalf("access token header must not be sent: %q", got)
		}
		gotClientIP = r.Header.Get("X-Real-IP")
		if cookie, err := r.Cookie(biz.RefreshTokenKey); err == nil {
			t.Fatalf("refresh token cookie must not be sent: %q", cookie.Value)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"order":{"order_id":"order-1"},"items":[],"address":{}}}`)
	}))
	defer server.Close()

	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:   1,
		ClientIP: "1.2.3.4",
	})
	client := NewOrderAPIClient(server.URL, time.Second)
	resp, err := client.CreateOrder(ctx, CreateOrderHTTPRequest{
		PreOrderID: "pre-1",
		AddressID:  9,
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if resp == nil || resp.Order.OrderID != "order-1" {
		t.Fatalf("resp=%+v", resp)
	}
	if gotClientIP != "127.0.0.1" {
		t.Fatalf("client ip header=%q", gotClientIP)
	}
	if gotBody == nil || gotBody["payment_method"] != float64(PaymentMethodAlipay) {
		t.Fatalf("body=%+v, want payment_method=%d", gotBody, PaymentMethodAlipay)
	}
	if gotBody["user_id"] != float64(1) {
		t.Fatalf("request body user_id=%v, want 1", gotBody["user_id"])
	}
}

func TestOrderAPIClientCancelOrder(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/douyin/order/internal/cancel" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"order_id":"order-1"}}`)
	}))
	defer server.Close()

	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:   8,
		ClientIP: "1.2.3.4",
	})
	client := NewOrderAPIClient(server.URL, time.Second)
	resp, err := client.CancelOrder(ctx, CancelOrderHTTPRequest{
		OrderID:      "order-1",
		CancelReason: "changed mind",
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if resp == nil || resp.OrderID != "order-1" {
		t.Fatalf("resp=%+v", resp)
	}
	if gotBody["user_id"] != float64(8) || gotBody["order_id"] != "order-1" || gotBody["cancel_reason"] != "changed mind" {
		t.Fatalf("body=%+v", gotBody)
	}
}

func TestOrderAPIClientBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":400,"msg":"bad request"}`)
	}))
	defer server.Close()

	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:   1,
		ClientIP: "1.2.3.4",
	})
	client := NewOrderAPIClient(server.URL, time.Second)
	_, err := client.CreateOrder(ctx, CreateOrderHTTPRequest{PreOrderID: "pre-1", AddressID: 9})
	if err == nil || !strings.Contains(err.Error(), "code=400") {
		t.Fatalf("err=%v, want business error", err)
	}
}

func TestOrderCancelHandlerUsesAPIAndTrustedUserID(t *testing.T) {
	fake := &fakeOrderAPI{}
	handler := orderCancelHandler(fake)
	result, err := handler(context.Background(), core.HandlerRequest{
		UserID: 7,
		Arguments: map[string]any{
			"user_id":  999,
			"order_id": "order-1",
			"reason":   "changed mind",
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if fake.cancelReq.UserID != 7 || fake.cancelReq.OrderID != "order-1" || fake.cancelReq.CancelReason != "changed mind" {
		t.Fatalf("cancelReq=%+v", fake.cancelReq)
	}
	data, ok := result.Data.(map[string]any)
	if !ok || data["order_id"] != "order-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestOrderCreateHandlerFixedAlipay(t *testing.T) {
	fake := &fakeOrderAPI{}
	handler := orderCreateHandler(fake)
	result, err := handler(context.Background(), core.HandlerRequest{
		UserID: 7,
		Arguments: map[string]any{
			"pre_order_id": "pre-1",
			"address_id":   int64(9),
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if fake.createReq.PaymentMethod != PaymentMethodAlipay || fake.createReq.PreOrderID != "pre-1" || fake.createReq.AddressID != 9 {
		t.Fatalf("req=%+v", fake.createReq)
	}
	if !strings.Contains(result.Summary, "支付宝") {
		t.Fatalf("summary=%q", result.Summary)
	}
}

type fakeOrderAPI struct {
	createReq CreateOrderHTTPRequest
	cancelReq CancelOrderHTTPRequest
}

func (f *fakeOrderAPI) CreateOrder(_ context.Context, req CreateOrderHTTPRequest) (*CreateOrderHTTPResponse, error) {
	f.createReq = req
	return &CreateOrderHTTPResponse{Order: OrderAPIOrder{OrderID: "order-1"}}, nil
}

func (f *fakeOrderAPI) CancelOrder(_ context.Context, req CancelOrderHTTPRequest) (*CancelOrderHTTPResponse, error) {
	f.cancelReq = req
	return &CancelOrderHTTPResponse{OrderID: req.OrderID}, nil
}

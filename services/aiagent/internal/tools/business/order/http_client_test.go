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
	var gotAccessToken, gotClientIP, gotRefreshToken string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccessToken = r.Header.Get(biz.TokenKey)
		gotClientIP = r.Header.Get("X-Real-IP")
		if cookie, err := r.Cookie(biz.RefreshTokenKey); err == nil {
			gotRefreshToken = cookie.Value
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"order":{"order_id":"order-1"},"items":[],"address":{}}}`)
	}))
	defer server.Close()

	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:       1,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ClientIP:     "1.2.3.4",
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
	if gotAccessToken != "access-token" || gotClientIP != "1.2.3.4" || gotRefreshToken != "refresh-token" {
		t.Fatalf("headers access=%q ip=%q refresh=%q", gotAccessToken, gotClientIP, gotRefreshToken)
	}
	if gotBody == nil || gotBody["payment_method"] != float64(PaymentMethodAlipay) {
		t.Fatalf("body=%+v, want payment_method=%d", gotBody, PaymentMethodAlipay)
	}
	if _, ok := gotBody["user_id"]; ok {
		t.Fatalf("request body must not contain user_id: %+v", gotBody)
	}
}

func TestOrderAPIClientBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":400,"msg":"bad request"}`)
	}))
	defer server.Close()

	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{
		UserID:       1,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ClientIP:     "1.2.3.4",
	})
	client := NewOrderAPIClient(server.URL, time.Second)
	_, err := client.CreateOrder(ctx, CreateOrderHTTPRequest{PreOrderID: "pre-1", AddressID: 9})
	if err == nil || !strings.Contains(err.Error(), "code=400") {
		t.Fatalf("err=%v, want business error", err)
	}
}

func TestOrderCreateHandlerFixedAlipay(t *testing.T) {
	fake := &fakeOrderCreateAPI{}
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
	if fake.req.PaymentMethod != PaymentMethodAlipay || fake.req.PreOrderID != "pre-1" || fake.req.AddressID != 9 {
		t.Fatalf("req=%+v", fake.req)
	}
	if !strings.Contains(result.Summary, "支付宝") {
		t.Fatalf("summary=%q", result.Summary)
	}
}

type fakeOrderCreateAPI struct {
	req CreateOrderHTTPRequest
}

func (f *fakeOrderCreateAPI) CreateOrder(_ context.Context, req CreateOrderHTTPRequest) (*CreateOrderHTTPResponse, error) {
	f.req = req
	return &CreateOrderHTTPResponse{Order: OrderAPIOrder{OrderID: "order-1"}}, nil
}

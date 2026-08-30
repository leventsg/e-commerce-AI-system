package order_tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/httpclient"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

const (
	orderCreatePath = "/douyin/order/internal/create"
	orderCancelPath = "/douyin/order/internal/cancel"

	// PaymentMethodAlipay keeps the AI order tool aligned with order-api.
	PaymentMethodAlipay int32 = 2
)

type CreateOrderHTTPRequest struct {
	UserID        uint32 `json:"user_id"`
	PreOrderID    string `json:"pre_order_id"`
	CouponID      string `json:"coupon_id,omitempty"`
	AddressID     int32  `json:"address_id"`
	PaymentMethod int32  `json:"payment_method"`
}

type CancelOrderHTTPRequest struct {
	UserID       uint32 `json:"user_id"`
	OrderID      string `json:"order_id"`
	CancelReason string `json:"cancel_reason,omitempty"`
}

type CreateOrderHTTPResponse struct {
	Order   OrderAPIOrder   `json:"order"`
	Items   []OrderAPIItem  `json:"items"`
	Address OrderAPIAddress `json:"address"`
}

type CancelOrderHTTPResponse struct {
	OrderID string `json:"order_id"`
}

type OrderAPIOrder struct {
	OrderID        string `json:"order_id"`
	PreOrderID     string `json:"pre_order_id"`
	UserID         uint32 `json:"user_id"`
	PaymentMethod  int32  `json:"payment_method"`
	TransactionID  string `json:"transaction_id"`
	PaidAt         int64  `json:"paid_at"`
	OriginalAmount string `json:"original_amount"`
	DiscountAmount string `json:"discount_amount"`
	PayableAmount  string `json:"payable_amount"`
	PaidAmount     string `json:"paid_amount"`
	OrderStatus    int32  `json:"order_status"`
	PaymentStatus  int32  `json:"payment_status"`
	Reason         string `json:"reason"`
	ExpireTime     string `json:"expire_time"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type OrderAPIItem struct {
	ItemID      string `json:"item_id"`
	ProductID   uint64 `json:"product_id"`
	Quantity    uint64 `json:"quantity"`
	ProductName string `json:"product_name"`
	ProductDesc string `json:"product_desc"`
	UnitPrice   string `json:"unit_price"`
}

type OrderAPIAddress struct {
	AddressID       uint64 `json:"address_id"`
	RecipientName   string `json:"recipient_name"`
	PhoneNumber     string `json:"phone_number"`
	Province        string `json:"province"`
	City            string `json:"city"`
	DetailedAddress string `json:"detailed_address"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	OrderID         string `json:"order_id"`
}

type OrderAPIClient struct {
	baseURL          string
	httpClient       *http.Client
	internalClientIP string
}

func NewOrderAPIClient(baseURL string, timeout time.Duration, internalClientIP ...string) *OrderAPIClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	clientIP := "127.0.0.1"
	if len(internalClientIP) > 0 && strings.TrimSpace(internalClientIP[0]) != "" {
		clientIP = strings.TrimSpace(internalClientIP[0])
	}
	return &OrderAPIClient{
		baseURL:          strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		internalClientIP: clientIP,
		httpClient:       &http.Client{Timeout: timeout},
	}
}

func (c *OrderAPIClient) CreateOrder(ctx context.Context, req CreateOrderHTTPRequest) (*CreateOrderHTTPResponse, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return nil, fmt.Errorf("%w: order api client not initialized", helper.ErrToolExecutionContext)
	}
	execution, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return nil, helper.ErrToolExecutionContext
	}
	if execution.ClientIP == "" {
		return nil, fmt.Errorf("%w: client ip missing", helper.ErrToolExecutionContext)
	}

	req.UserID = uint32(execution.UserID)
	req.PaymentMethod = PaymentMethodAlipay
	var out CreateOrderHTTPResponse
	if err := httpclient.DoJSONEnvelope(ctx, http.MethodPost, c.baseURL+orderCreatePath, req, &out, c.requestOptions(execution.ClientIP)...); err != nil {
		return nil, err
	}
	if out.Order.OrderID == "" {
		return nil, fmt.Errorf("order_create http returned empty order")
	}
	return &out, nil
}

func (c *OrderAPIClient) CancelOrder(ctx context.Context, req CancelOrderHTTPRequest) (*CancelOrderHTTPResponse, error) {
	if c == nil || c.httpClient == nil || c.baseURL == "" {
		return nil, fmt.Errorf("%w: order api client not initialized", helper.ErrToolExecutionContext)
	}
	execution, ok := helper.ToolExecutionFromContext(ctx)
	if !ok || execution.UserID == 0 {
		return nil, helper.ErrToolExecutionContext
	}
	if execution.ClientIP == "" {
		return nil, fmt.Errorf("%w: client ip missing", helper.ErrToolExecutionContext)
	}

	req.UserID = uint32(execution.UserID)
	var out CancelOrderHTTPResponse
	if err := httpclient.DoJSONEnvelope(ctx, http.MethodPost, c.baseURL+orderCancelPath, req, &out, c.requestOptions(execution.ClientIP)...); err != nil {
		return nil, err
	}
	if out.OrderID == "" {
		return nil, fmt.Errorf("order_cancel http returned empty order")
	}
	return &out, nil
}

func (c *OrderAPIClient) requestOptions(clientIP string) []httpclient.Option {
	ip := c.internalClientIP
	if ip == "" {
		ip = clientIP
	}
	return []httpclient.Option{
		httpclient.WithClient(c.httpClient),
		httpclient.WithHeader("X-Real-IP", ip),
	}
}

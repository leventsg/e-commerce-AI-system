package order_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

const (
	orderCreatePath = "/douyin/order/create"
	maxOrderAPIResp = 1 << 20

	// PaymentMethodAlipay 固定使用支付宝，与现有 order-api 硬编码逻辑保持一致。
	PaymentMethodAlipay int32 = 2
)

type CreateOrderHTTPRequest struct {
	PreOrderID    string `json:"pre_order_id"`
	CouponID      string `json:"coupon_id,omitempty"`
	AddressID     int32  `json:"address_id"`
	PaymentMethod int32  `json:"payment_method"`
}

type CreateOrderHTTPResponse struct {
	Order   OrderAPIOrder   `json:"order"`
	Items   []OrderAPIItem  `json:"items"`
	Address OrderAPIAddress `json:"address"`
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

type orderAPIEnvelope struct {
	Code int                     `json:"code"`
	Msg  string                  `json:"msg"`
	Data CreateOrderHTTPResponse `json:"data"`
}

type OrderAPIClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewOrderAPIClient(baseURL string, timeout time.Duration) *OrderAPIClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &OrderAPIClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
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
	if strings.TrimSpace(execution.AccessToken) == "" {
		return nil, fmt.Errorf("%w: access token missing", helper.ErrToolExecutionContext)
	}
	if strings.TrimSpace(execution.RefreshToken) == "" {
		return nil, fmt.Errorf("%w: refresh token missing", helper.ErrToolExecutionContext)
	}
	if strings.TrimSpace(execution.ClientIP) == "" {
		return nil, fmt.Errorf("%w: client ip missing", helper.ErrToolExecutionContext)
	}

	req.PaymentMethod = PaymentMethodAlipay
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal order create request: %v", helper.ErrInvalidToolArguments, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+orderCreatePath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(biz.TokenKey, execution.AccessToken)
	httpReq.Header.Set("X-Real-IP", execution.ClientIP)
	httpReq.AddCookie(&http.Cookie{Name: biz.RefreshTokenKey, Value: execution.RefreshToken})

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOrderAPIResp))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("order_create http status %d: %s", resp.StatusCode, truncateOrderAPIError(raw))
	}

	var envelope orderAPIEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode order_create response: %w", err)
	}
	if envelope.Code != 0 {
		msg := strings.TrimSpace(envelope.Msg)
		if msg == "" {
			msg = "order_create http business error"
		}
		return nil, fmt.Errorf("order_create http code=%d msg=%s", envelope.Code, msg)
	}
	if strings.TrimSpace(envelope.Data.Order.OrderID) == "" {
		return nil, fmt.Errorf("order_create http returned empty order")
	}
	return &envelope.Data, nil
}

func truncateOrderAPIError(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}

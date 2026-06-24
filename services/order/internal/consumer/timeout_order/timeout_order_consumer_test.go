package timeout_order

import (
	"testing"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	service_order "github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/stretchr/testify/require"
)

func TestShouldCloseTimeoutOrderBySource(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		orderStatus   service_order.OrderStatus
		paymentStatus service_order.PaymentStatus
		want          bool
	}{
		{
			name:          "order timeout closes created unpaid order",
			source:        biz.TimeoutSourceOrder,
			orderStatus:   service_order.OrderStatus_ORDER_STATUS_CREATED,
			paymentStatus: service_order.PaymentStatus_PAYMENT_STATUS_NOT_PAID,
			want:          true,
		},
		{
			name:          "order timeout skips paying order",
			source:        biz.TimeoutSourceOrder,
			orderStatus:   service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT,
			paymentStatus: service_order.PaymentStatus_PAYMENT_STATUS_PAYING,
			want:          false,
		},
		{
			name:          "payment timeout closes paying order",
			source:        biz.TimeoutSourcePaymentTimeout,
			orderStatus:   service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT,
			paymentStatus: service_order.PaymentStatus_PAYMENT_STATUS_PAYING,
			want:          true,
		},
		{
			name:          "payment failed closes paying order",
			source:        biz.TimeoutSourcePaymentFailed,
			orderStatus:   service_order.OrderStatus_ORDER_STATUS_PENDING_PAYMENT,
			paymentStatus: service_order.PaymentStatus_PAYMENT_STATUS_PAYING,
			want:          true,
		},
		{
			name:          "payment timeout skips created unpaid order",
			source:        biz.TimeoutSourcePaymentTimeout,
			orderStatus:   service_order.OrderStatus_ORDER_STATUS_CREATED,
			paymentStatus: service_order.PaymentStatus_PAYMENT_STATUS_NOT_PAID,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldCloseTimeoutOrder(tt.source, tt.orderStatus, tt.paymentStatus))
		})
	}
}

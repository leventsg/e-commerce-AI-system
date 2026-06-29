package biz

import "time"

const (
	OrderRpcPort = 10004
)
const (
	OrderExpireTime    = time.Minute * 30
	CheckoutExpireTime = time.Minute * 1
)

const (
	OrderTimeoutZSetKey          = "order:timeout:zset"
	PaymentTimeoutZSetKey        = "payment:timeout:zset"
	CheckoutTimeoutZSetKey       = "checkout:timeout:zset"
	TimeoutSourceOrder           = "order_timeout"
	TimeoutSourcePaymentTimeout  = "payment_timeout"
	TimeoutSourcePaymentFailed   = "payment_failed"
	TimeoutSourceCheckout        = "checkout_timeout"
	TimeoutOrderEventType        = "order.timeout"
	PaymentTimeoutEventType      = "payment.timeout"
	PaymentFailedEventType       = "payment.failed"
	CheckoutTimeoutEventType     = "checkout.timeout"
	DefaultOutboxMaxRetry        = 10
	OrderTimeoutScanBatchSize    = 100
	OrderTimeoutScanIntervalTime = time.Second
	CheckoutTimeoutRetryDelay    = 30 * time.Second
	PaymentExpireTime            = time.Minute * 15
)

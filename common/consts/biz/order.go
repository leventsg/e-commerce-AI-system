package biz

import "time"

const (
	OrderRpcPort = 10004
)
const (
	OrderExpireTime = time.Minute * 30
)

const (
	OrderTimeoutZSetKey          = "order:timeout:zset"
	TimeoutSourceOrder           = "order_timeout"
	TimeoutSourcePaymentTimeout  = "payment_timeout"
	TimeoutSourcePaymentFailed   = "payment_failed"
	TimeoutOrderEventType        = "order.timeout"
	PaymentTimeoutEventType      = "payment.timeout"
	PaymentFailedEventType       = "payment.failed"
	DefaultOutboxMaxRetry        = 10
	OrderTimeoutScanBatchSize    = 100
	OrderTimeoutScanIntervalTime = time.Second
	PaymentExpireTime            = time.Minute * 15
)

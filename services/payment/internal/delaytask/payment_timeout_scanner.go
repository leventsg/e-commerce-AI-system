package delaytask

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	paymentmodel "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/payment/payment"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type PaymentTimeoutScanner struct {
	redis     *redis.Redis
	model     sqlx.SqlConn
	payments  paymentmodel.PaymentsModel
	orderRpc  orderservice.OrderService
	batchSize int
	interval  time.Duration
	zsetKey   string
}

func NewPaymentTimeoutScanner(
	rdb *redis.Redis,
	model sqlx.SqlConn,
	payments paymentmodel.PaymentsModel,
	orderRpc orderservice.OrderService,
) *PaymentTimeoutScanner {
	return &PaymentTimeoutScanner{
		redis:     rdb,
		model:     model,
		payments:  payments,
		orderRpc:  orderRpc,
		batchSize: biz.OrderTimeoutScanBatchSize,
		interval:  biz.OrderTimeoutScanIntervalTime,
		zsetKey:   biz.PaymentTimeoutZSetKey,
	}
}

func (s *PaymentTimeoutScanner) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		if err := s.ScanOnce(ctx); err != nil {
			logx.Errorw("scan payment timeout task failed", logx.Field("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *PaymentTimeoutScanner) ScanOnce(ctx context.Context) error {
	if s == nil || s.redis == nil || s.model == nil || s.payments == nil || s.orderRpc == nil {
		return nil
	}
	now := time.Now().Unix()
	// 批量获取超时订单ID
	pairs, err := s.redis.ZrangebyscoreWithScoresAndLimitCtx(ctx, s.zsetKey, 0, now, 0, s.batchSize)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := s.handlePayment(ctx, pair.Key); err != nil {
			logx.Errorw("handle expired payment timeout task failed",
				logx.Field("err", err),
				logx.Field("order_id", pair.Key))
		}
	}
	return nil
}

// 处理单个超时支付单
func (s *PaymentTimeoutScanner) handlePayment(ctx context.Context, orderID string) error {
	paymentRes, removeTask, err := s.expirePaymentIfNeeded(ctx, orderID)
	if err != nil {
		return err
	}
	// 如果支付单不存在或状态不匹配，则直接移除延时任务
	if paymentRes == nil {
		if removeTask {
			_, err = s.redis.ZremCtx(ctx, s.zsetKey, orderID)
		}
		return err
	}
	if shouldNotifyOrder(payment.PaymentStatus(paymentRes.Status)) {
		// 通知订单服务处理支付超时订单
		resp, err := s.orderRpc.HandlePaymentTimeoutOrder(ctx, &order.HandlePaymentTimeoutOrderRequest{
			OrderId: paymentRes.OrderId.String,
			UserId:  int32(paymentRes.UserId),
			Source:  biz.TimeoutSourcePaymentTimeout,
		})
		if err != nil {
			return err
		}
		if resp != nil && resp.StatusCode != code.Success {
			return fmt.Errorf("handle payment timeout order failed: status_code=%d status_msg=%s", resp.StatusCode, resp.StatusMsg)
		}
	}
	if removeTask {
		_, err = s.redis.ZremCtx(ctx, s.zsetKey, orderID)
	}
	return err
}

// 检查支付单状态，如果是未支付则更新为已超时，并返回支付单信息和是否需要移除延时任务的标志
func (s *PaymentTimeoutScanner) expirePaymentIfNeeded(ctx context.Context, orderID string) (*paymentmodel.Payments, bool, error) {
	var paymentRes *paymentmodel.Payments
	removeTask := true
	if err := s.model.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		paymentsModel := s.payments.WithSession(session)
		pRes, err := paymentsModel.FindOneByOrderIdWithLock(ctx, orderID)
		if err != nil {
			if errors.Is(err, paymentmodel.ErrNotFound) {
				logx.Infow("payment timeout skipped, payment not found", logx.Field("order_id", orderID))
				return nil
			}
			return err
		}
		action := paymentTimeoutAction(payment.PaymentStatus(pRes.Status))
		// 如果支付单状态为未支付，则更新为已超时
		if action.expirePayment {
			if err := paymentsModel.UpdateStatusByOrderId(ctx, orderID, int64(payment.PaymentStatus_PAYMENT_STATUS_EXPIRED)); err != nil {
				return err
			}
			pRes.Status = int64(payment.PaymentStatus_PAYMENT_STATUS_EXPIRED)
		}
		removeTask = action.removeTask
		paymentRes = pRes
		return nil
	}); err != nil {
		return nil, false, err
	}
	return paymentRes, removeTask, nil
}

// 支付单超时的决策结果
type paymentTimeoutDecision struct {
	expirePayment bool
	notifyOrder   bool
	removeTask    bool
}

func paymentTimeoutAction(status payment.PaymentStatus) paymentTimeoutDecision {
	switch status {
	case payment.PaymentStatus_PAYMENT_STATUS_UNPAID:
		return paymentTimeoutDecision{expirePayment: true, notifyOrder: true, removeTask: true}
	case payment.PaymentStatus_PAYMENT_STATUS_EXPIRED:
		return paymentTimeoutDecision{notifyOrder: true, removeTask: true}
	default:
		return paymentTimeoutDecision{removeTask: true}
	}
}

func shouldNotifyOrder(status payment.PaymentStatus) bool {
	return paymentTimeoutAction(status).notifyOrder
}

package mq

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	paymentmodel "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	paymenttimeout "github.com/leventsg/e-commerce-AI-system/services/payment/internal/timeout"
	"github.com/leventsg/e-commerce-AI-system/services/payment/payment"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func (a *PaymentDelayMQ) consumer(ctx context.Context) {

	ch, err := a.conn.Channel()
	if err != nil {
		logx.Errorw("Failed to open a channel", logx.Field("err", err))
		return
	}

	results, err := ch.Consume(
		QueueName, // 队列名称
		"",        // 消费者标签
		false,     // 自动确认（ack）
		false,     // 排他性
		false,     // 本地消息
		false,     // 等待确认
		nil,       // 参数
	)
	if err != nil {
		logx.Errorw("Failed to register a consumer", logx.Field("err", err))
	}
	logx.Infow("Starting RabbitMQ consumer...")

	for res := range results {
		var msg *PaymentReq
		if err := json.Unmarshal(res.Body, &msg); err != nil {
			logx.Errorw("failed to unmarshal message", logx.Field("error", err), logx.Field("body", string(res.Body)))
			if err := res.Reject(false); err != nil {
				logx.Errorw("failed to reject message", logx.Field("error", err), logx.Field("body", string(res.Body)))
			}
			continue
		}
		if err := a.expirePayment(ctx, msg.OrderId); err != nil {
			logx.Errorw("expire payment failed",
				logx.Field("err", err),
				logx.Field("order_id", msg.OrderId))
			if err := res.Reject(true); err != nil {
				logx.Errorw("failed to reject message", logx.Field("error", err), logx.Field("body", string(res.Body)))
			}
			continue
		}
		if err := res.Ack(false); err != nil {
			logx.Errorw("failed to ack message", logx.Field("error", err), logx.Field("body", string(res.Body)))
		}

	}
}

func (a *PaymentDelayMQ) expirePayment(ctx context.Context, orderID string) error {
	if a == nil || a.model == nil || a.payments == nil || a.outbox == nil {
		return nil
	}
	return a.model.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		paymentsModel := a.payments.WithSession(session)
		paymentRes, err := paymentsModel.FindOneByOrderIdWithLock(ctx, orderID)
		if err != nil {
			if errors.Is(err, paymentmodel.ErrNotFound) {
				logx.Infow("payment timeout skipped, payment not found", logx.Field("order_id", orderID))
				return nil
			}
			return err
		}
		if payment.PaymentStatus(paymentRes.Status) != payment.PaymentStatus_PAYMENT_STATUS_UNPAID {
			logx.Infow("payment timeout skipped",
				logx.Field("order_id", orderID),
				logx.Field("payment_status", paymentRes.Status))
			return nil
		}
		if err := paymentsModel.UpdateStatusByOrderId(ctx, orderID, int64(payment.PaymentStatus_PAYMENT_STATUS_EXPIRED)); err != nil {
			return err
		}
		return paymenttimeout.SaveOrderTimeoutOutbox(
			ctx,
			session,
			a.config,
			a.outbox,
			paymentRes,
			biz.PaymentTimeoutEventType,
			biz.TimeoutSourcePaymentTimeout,
		)
	})
}

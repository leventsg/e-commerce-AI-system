package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	paymentM "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/delaytask"
	paymentevent "github.com/leventsg/e-commerce-AI-system/services/payment/internal/event"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/server"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/payment/payment"
	"github.com/smartwalle/alipay/v3"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/payment.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		payment.RegisterPaymentServer(grpcServer, server.NewPaymentServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	if err := consul.RegisterService(c.ListenOn, c.Consul); err != nil {
		logx.Errorw("register service error", logx.Field("err", err))
		panic(err)
	}
	paymentSvc := NewPaymentService(ctx)
	paymentSvc.startHTTPServer()

	defer s.Stop()

	// 初始化支付超时订单消息投递器，定时扫描并投递消息到mq (mysql -> mq)
	outboxCtx, cancelOutbox := context.WithCancel(context.Background())
	defer cancelOutbox()
	if ctx.Outbox != nil {
		go ctx.Outbox.Run(outboxCtx)
	}

	// 初始化支付超时扫描器，定时扫描超时支付单并通知订单服务处理 (redis -> mysql)
	timeoutScannerCtx, cancelTimeoutScanner := context.WithCancel(context.Background())
	defer cancelTimeoutScanner()
	timeoutScanner := delaytask.NewPaymentTimeoutScanner(ctx.Rdb, ctx.Model, ctx.PaymentModel, ctx.OrderRpc)
	go timeoutScanner.Run(timeoutScannerCtx)

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}

type PaymentService struct {
	ctx *svc.ServiceContext
}

func NewPaymentService(ctx *svc.ServiceContext) *PaymentService {
	return &PaymentService{ctx: ctx}
}

// 封装支付宝通知处理
func (s *PaymentService) handleAlipayNotification(writer http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		logx.Infow("Failed to parse form", logx.Field("err", err))
		return
	}
	// DecodeNotification 内部已调用 VerifySign 方法验证签名
	var notify, err = s.ctx.Alipay.DecodeNotification(request.Form)
	if err != nil {
		logx.Errorw("Failed to decode notification", logx.Field("err", err))
		return
	}
	// 根据通知状态处理业务逻辑
	switch notify.TradeStatus {
	case "TRADE_FINISHED":
	// 交易完成（不可退款）
	case "TRADE_CLOSED":
		// 未付款交易超时关闭
		logx.Infow("Payment closed", logx.Field("order_id", notify.OutTradeNo))
		if err := s.closeUnpaidPayment(request.Context(), notify.OutTradeNo); err != nil {
			logx.Errorw("close unpaid payment failed", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
			return
		}
	case "TRADE_SUCCESS":
		logx.Infow("Payment success", logx.Field("order_id", notify.OutTradeNo))
		// 使用消息队列使用
		// 解析时间字符串
		paymentTime, err := time.Parse(time.DateTime, notify.GmtPayment)
		if err != nil {
			logx.Errorw("Failed to parse time", logx.Field("err", err))
			return
		}
		var paymentRes *paymentM.Payments
		shouldPublishPaymentSuccess := false
		timestamp := paymentTime.Unix()
		if err := s.ctx.Model.TransactCtx(request.Context(), func(ctx context.Context, session sqlx.Session) error {
			paymentsModel := s.ctx.PaymentModel.WithSession(session)
			pRes, err := paymentsModel.FindOneByOrderId(ctx, notify.OutTradeNo)
			paymentRes = pRes
			if err != nil {
				logx.Errorw("Failed to find payment record", logx.Field("err", err))
				return err
			}
			switch payment.PaymentStatus(pRes.Status) {
			// 订单状态为待支付时，更新订单状态为已支付，退款
			case payment.PaymentStatus_PAYMENT_STATUS_EXPIRED:
				logx.Infow("payment success skipped because payment expired", logx.Field("order_id", notify.OutTradeNo))
			case payment.PaymentStatus_PAYMENT_STATUS_UNPAID:
				// 更新支付状态为支付已成功
				if err := paymentsModel.UpdateInfoByOrderId(ctx, &paymentM.Payments{
					OrderId:       sql.NullString{String: notify.OutTradeNo, Valid: true}, // 支付成功后更新
					TransactionId: sql.NullString{String: notify.TradeNo, Valid: true},
					Status:        int64(payment.PaymentStatus_PAYMENT_STATUS_PAID),
					PaidAt:        sql.NullInt64{Int64: timestamp},
				}); err != nil {
					return err
				}
				paymentRes.TransactionId = sql.NullString{String: notify.TradeNo, Valid: true}
				paymentRes.Status = int64(payment.PaymentStatus_PAYMENT_STATUS_PAID)
				paymentRes.PaidAt = sql.NullInt64{Int64: timestamp, Valid: true}
				shouldPublishPaymentSuccess = true
				//状态异常，退款操作
			case payment.PaymentStatus_PAYMENT_STATUS_PAID:
				// 支付单状态已经是已支付，对非正确的支付单状态进行更改
				// 这里设置为true，是防止首次处理时消息投递失败但数据库已更新的情况
				shouldPublishPaymentSuccess = true
				if !paymentRes.TransactionId.Valid || paymentRes.TransactionId.String == "" {
					paymentRes.TransactionId = sql.NullString{String: notify.TradeNo, Valid: true}
				}
				if !paymentRes.PaidAt.Valid || paymentRes.PaidAt.Int64 == 0 {
					paymentRes.PaidAt = sql.NullInt64{Int64: timestamp, Valid: true}
				}
			}
			return nil
		}); err != nil {
			logx.Errorw("Failed to update payment record", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
			return
		}

		// 支付单不存在或已处理，则跳过
		if paymentRes == nil || !shouldPublishPaymentSuccess {
			logx.Infow("payment success event skipped",
				logx.Field("order_id", notify.OutTradeNo),
				logx.Field("should_publish", shouldPublishPaymentSuccess))
			return
		}
		// 获取订单详情和优惠券信息
		orderDetail, err := s.ensureOrderPaidAndGetDetail(request.Context(), notify.OutTradeNo, paymentRes)
		if err != nil {
			logx.Errorw("get order detail for payment success event failed", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
			return
		}
		couponID, err := s.findOrderCouponID(request.Context(), notify.OutTradeNo, int32(paymentRes.UserId))
		if err != nil {
			logx.Errorw("get order coupon for payment success event failed", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
			return
		}
		// 保存支付成功事件到 outbox 表，等待异步投递到消息队列
		if err := paymentevent.SavePaymentSuccessOutbox(
			request.Context(),
			nil,
			s.ctx.Config,
			s.ctx.PaymentOutboxModel,
			orderDetail,
			couponID,
		); err != nil {
			logx.Errorw("save payment success outbox failed", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
			return
		}
		// 删除redis中支付超时任务
		if _, err := s.ctx.Rdb.ZremCtx(request.Context(), biz.PaymentTimeoutZSetKey, notify.OutTradeNo); err != nil {
			logx.Errorw("delete payment timeout task failed", logx.Field("err", err), logx.Field("order_id", notify.OutTradeNo))
		}

	}
	// 返回确认响应给支付宝
	alipay.ACKNotification(writer)

}

// 支付超时处理
func (s *PaymentService) closeUnpaidPayment(ctx context.Context, orderID string) error {
	var paymentRes *paymentM.Payments
	shouldNotifyOrder := false
	if err := s.ctx.Model.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		paymentsModel := s.ctx.PaymentModel.WithSession(session)
		pRes, err := paymentsModel.FindOneByOrderIdWithLock(ctx, orderID)
		if err != nil {
			return err
		}
		paymentRes = pRes
		switch payment.PaymentStatus(pRes.Status) {
		case payment.PaymentStatus_PAYMENT_STATUS_UNPAID:
			// 更新支付单状态为已超时
			if err := paymentsModel.UpdateStatusByOrderId(ctx, orderID, int64(payment.PaymentStatus_PAYMENT_STATUS_EXPIRED)); err != nil {
				return err
			}
			pRes.Status = int64(payment.PaymentStatus_PAYMENT_STATUS_EXPIRED)
			shouldNotifyOrder = true
		case payment.PaymentStatus_PAYMENT_STATUS_EXPIRED:
			shouldNotifyOrder = true
		default:
			logx.Infow("trade closed skipped",
				logx.Field("order_id", orderID),
				logx.Field("payment_status", pRes.Status))
			return nil
		}
		return nil
	}); err != nil {
		return err
	}
	if !shouldNotifyOrder || paymentRes == nil {
		return nil
	}
	resp, err := s.ctx.OrderRpc.HandlePaymentTimeoutOrder(ctx, &order.HandlePaymentTimeoutOrderRequest{
		OrderId: paymentRes.OrderId.String,
		UserId:  int32(paymentRes.UserId),
		Source:  biz.TimeoutSourcePaymentFailed,
	})
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		return fmt.Errorf("handle payment failed order failed: status_code=%d status_msg=%s", resp.StatusCode, resp.StatusMsg)
	}
	if _, err := s.ctx.Rdb.ZremCtx(ctx, biz.PaymentTimeoutZSetKey, orderID); err != nil {
		logx.Errorw("delete payment timeout task failed", logx.Field("err", err), logx.Field("order_id", orderID))
	}
	return nil
}

func (s *PaymentService) ensureOrderPaidAndGetDetail(ctx context.Context, orderID string, paymentRes *paymentM.Payments) (*order.OrderDetailResponse, error) {
	if paymentRes == nil {
		return nil, errors.New("payment record is nil")
	}
	orderDetail, err := s.ctx.OrderRpc.GetOrder(ctx, &order.GetOrderRequest{
		OrderId: orderID,
		UserId:  uint32(paymentRes.UserId),
	})
	if err != nil {
		return nil, err
	}
	if err := validateOrderDetail(orderDetail); err != nil {
		return nil, err
	}
	if orderDetail.Order.OrderStatus == order.OrderStatus_ORDER_STATUS_PAID &&
		orderDetail.Order.PaymentStatus == order.PaymentStatus_PAYMENT_STATUS_PAID {
		return orderDetail, nil
	}

	// 更新订单状态为已支付
	orderRes, err := s.ctx.OrderRpc.UpdateOrder2PaymentSuccess(ctx, &order.UpdateOrder2PaymentSuccessRequest{
		OrderId: orderID,
		PaymentResult: &order.PaymentResult{
			TransactionId: paymentRes.TransactionId.String,
			PaidAmount:    paymentRes.PaidAmount.Int64,
			PaidAt:        paymentRes.PaidAt.Int64,
		},
		UserId: int32(paymentRes.UserId),
	})
	if err != nil {
		return nil, err
	}
	if orderRes == nil || orderRes.StatusCode != code.Success {
		if orderRes == nil {
			return nil, errors.New("update order status returned nil response")
		}
		return nil, fmt.Errorf("update order status failed: status_code=%d status_msg=%s", orderRes.StatusCode, orderRes.StatusMsg)
	}

	orderDetail, err = s.ctx.OrderRpc.GetOrder(ctx, &order.GetOrderRequest{
		OrderId: orderID,
		UserId:  uint32(paymentRes.UserId),
	})
	if err != nil {
		return nil, err
	}
	if err := validateOrderDetail(orderDetail); err != nil {
		return nil, err
	}
	return orderDetail, nil
}

func validateOrderDetail(orderDetail *order.OrderDetailResponse) error {
	if orderDetail == nil {
		return errors.New("order detail response is nil")
	}
	if orderDetail.StatusCode != code.Success {
		return fmt.Errorf("get order detail failed: status_code=%d status_msg=%s", orderDetail.StatusCode, orderDetail.StatusMsg)
	}
	if orderDetail.Order == nil {
		return errors.New("order detail missing order")
	}
	return nil
}

func (s *PaymentService) findOrderCouponID(ctx context.Context, orderID string, userID int32) (string, error) {
	var couponID sql.NullString
	// 从订单表中查询优惠券ID
	err := s.ctx.Model.QueryRowCtx(ctx, &couponID, "select `coupon_id` from `orders` where `order_id` = ? and `user_id` = ? limit 1", orderID, userID)
	if err != nil {
		return "", err
	}
	if !couponID.Valid {
		return "", nil
	}
	return couponID.String, nil
}

// 封装HTTP服务启动
func (s *PaymentService) startHTTPServer() {
	http.HandleFunc(s.ctx.Config.Alipay.NotifyPath, s.handleAlipayNotification)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", s.ctx.Config.Alipay.NotifyPort), nil); err != nil {
			logx.Errorw("http server error", logx.Field("err", err))
		}
	}()
}

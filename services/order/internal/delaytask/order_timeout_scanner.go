package delaytask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	order2 "github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/event"
	service_order "github.com/leventsg/e-commerce-AI-system/services/order/order"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// OrderTimeoutScanner 订单超时扫描器
// 负责从Redis ZSet中扫描超时订单，并将其写入Outbox消息表
// 采用轮询机制定期检查，支持批量处理和重试策略
type OrderTimeoutScanner struct {
	config     config.Config
	redis      *redis.Redis
	orders     order2.OrdersModel
	orderItems order2.OrderItemsModel
	outbox     order2.OutboxMessagesModel
	batchSize  int
	interval   time.Duration
	zsetKey    string
	maxRetries int
}

func NewOrderTimeoutScanner(c config.Config, rdb *redis.Redis, orders order2.OrdersModel, orderItems order2.OrderItemsModel, outbox order2.OutboxMessagesModel) *OrderTimeoutScanner {
	maxRetry := c.Outbox.MaxRetry
	if maxRetry <= 0 {
		maxRetry = biz.DefaultOutboxMaxRetry
	}
	return &OrderTimeoutScanner{
		config:     c,
		redis:      rdb,
		orders:     orders,
		orderItems: orderItems,
		outbox:     outbox,
		batchSize:  biz.OrderTimeoutScanBatchSize,
		interval:   biz.OrderTimeoutScanIntervalTime,
		zsetKey:    biz.OrderTimeoutZSetKey,
		maxRetries: maxRetry,
	}
}

func (s *OrderTimeoutScanner) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		if err := s.ScanOnce(ctx); err != nil {
			logx.Errorw("scan order timeout task failed", logx.Field("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ScanOnce 执行一次超时订单扫描，批量获取后逐个处理
func (s *OrderTimeoutScanner) ScanOnce(ctx context.Context) error {
	if s == nil || s.redis == nil || s.orders == nil || s.orderItems == nil || s.outbox == nil {
		return nil
	}
	now := time.Now().Unix()
	pairs, err := s.redis.ZrangebyscoreWithScoresAndLimitCtx(ctx, s.zsetKey, 0, now, 0, s.batchSize)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := s.handleOrder(ctx, pair.Key); err != nil {
			logx.Errorw("handle expired order timeout task failed",
				logx.Field("err", err),
				logx.Field("order_id", pair.Key))
		}
	}
	return nil
}

// handleOrder 处理单个超时订单
func (s *OrderTimeoutScanner) handleOrder(ctx context.Context, orderID string) error {
	orderRes, err := s.orders.FindOne(ctx, orderID)
	if err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			_, remErr := s.redis.ZremCtx(ctx, s.zsetKey, orderID)
			return remErr
		}
		return err
	}

	// 查询订单状态，判断是否需要写入超时消息
	if !shouldWriteOrderTimeout(orderRes) {
		_, err := s.redis.ZremCtx(ctx, s.zsetKey, orderID)
		return err
	}
	// 保存超时订单到Outbox消息表
	if err := s.saveTimeoutOutbox(ctx, orderRes); err != nil {
		return err
	}
	// 移除已处理的订单ID
	_, err = s.redis.ZremCtx(ctx, s.zsetKey, orderID)
	return err
}

func shouldWriteOrderTimeout(orderRes *order2.Orders) bool {
	if orderRes == nil {
		return false
	}
	// 只有在订单状态为已创建且支付状态为未支付时，才写入超时消息
	return service_order.OrderStatus(orderRes.OrderStatus) == service_order.OrderStatus_ORDER_STATUS_CREATED &&
		service_order.PaymentStatus(orderRes.PaymentStatus) == service_order.PaymentStatus_PAYMENT_STATUS_NOT_PAID
}

// saveTimeoutOutbox 保存超时订单到Outbox消息表
func (s *OrderTimeoutScanner) saveTimeoutOutbox(ctx context.Context, orderRes *order2.Orders) error {
	kafkaConfig, err := s.config.KafkaMQ.TopicConfig("TimeoutOrders")
	if err != nil {
		return err
	}
	orderItems, err := s.orderItems.QueryOrderItemsByOrderID(ctx, orderRes.OrderId)
	if err != nil {
		return err
	}
	if len(orderItems) == 0 {
		return errors.New("timeout order outbox missing order items")
	}
	payload, err := json.Marshal(&event.TimeoutOrder{
		OrderId:    orderRes.OrderId,
		UserId:     int32(orderRes.UserId),
		Source:     biz.TimeoutSourceOrder,
		PreOrderId: orderRes.PreOrderId,
		CouponId:   orderRes.CouponId,
		Items:      convertToInventoryItems(orderItems),
	})
	if err != nil {
		return err
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		messageID = uuid.New()
	}
	_, err = s.outbox.Insert(ctx, &order2.OutboxMessages{
		MessageId:     messageID.String(),
		EventType:     biz.TimeoutOrderEventType,
		Topic:         kafkaConfig.Topic,
		MessageKey:    orderRes.OrderId,
		Payload:       string(payload),
		Status:        order2.OutboxStatusPending,
		RetryCount:    0,
		MaxRetryCount: int64(s.maxRetries),
		NextRetryAt:   time.Now(),
		LockedUntil:   sql.NullTime{},
		LastError:     sql.NullString{},
		SentAt:        sql.NullTime{},
	})
	return err
}

func convertToInventoryItems(orderItems []*order2.OrderItems) []*inventory.InventoryReq_Items {
	inventoryItems := make([]*inventory.InventoryReq_Items, len(orderItems))
	for i, item := range orderItems {
		inventoryItems[i] = &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		}
	}
	return inventoryItems
}

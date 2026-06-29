package delaytask

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/mq"
	checkoutmodel "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	servicecheckout "github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/event"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type checkoutTimeoutRedis interface {
	ZrangebyscoreWithScoresAndLimitCtx(ctx context.Context, key string, start, stop int64, page, size int) ([]redis.Pair, error)
	ZremCtx(ctx context.Context, key string, values ...any) (int, error)
	ZaddCtx(ctx context.Context, key string, score int64, value string) (bool, error)
}

type checkoutFinder interface {
	FindOne(ctx context.Context, preOrderId string) (*checkoutmodel.Checkouts, error)
}

type checkoutItemsFinder interface {
	FindItemsByPreOrder(ctx context.Context, preOrderId string) ([]*checkoutmodel.CheckoutItems, error)
}

type CheckoutTimeoutScanner struct {
	redis      checkoutTimeoutRedis
	checkouts  checkoutFinder
	items      checkoutItemsFinder
	producer   mq.Producer
	topic      string
	batchSize  int
	interval   time.Duration
	retryDelay time.Duration
	now        func() time.Time
}

func NewCheckoutTimeoutScanner(
	c config.Config,
	rdb *redis.Redis,
	checkouts checkoutmodel.CheckoutsModel,
	items checkoutmodel.CheckoutItemsModel,
	producer mq.Producer,
) *CheckoutTimeoutScanner {
	kafkaConfig, err := c.KafkaMQ.TopicConfig("CheckoutTimeouts")
	if err != nil {
		logx.Errorw("get checkout-timeouts kafka config failed", logx.Field("err", err))
		return nil
	}
	return NewCheckoutTimeoutScannerWithDeps(
		rdb,
		checkouts,
		items,
		producer,
		kafkaConfig.Topic,
		biz.CheckoutTimeoutRetryDelay,
		time.Now,
	)
}

func NewCheckoutTimeoutScannerWithDeps(
	rdb checkoutTimeoutRedis,
	checkouts checkoutFinder,
	items checkoutItemsFinder,
	producer mq.Producer,
	topic string,
	retryDelay time.Duration,
	now func() time.Time,
) *CheckoutTimeoutScanner {
	if retryDelay <= 0 {
		retryDelay = biz.CheckoutTimeoutRetryDelay
	}
	if now == nil {
		now = time.Now
	}
	return &CheckoutTimeoutScanner{
		redis:      rdb,
		checkouts:  checkouts,
		items:      items,
		producer:   producer,
		topic:      topic,
		batchSize:  biz.OrderTimeoutScanBatchSize,
		interval:   biz.OrderTimeoutScanIntervalTime,
		retryDelay: retryDelay,
		now:        now,
	}
}

func (s *CheckoutTimeoutScanner) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if err := s.ScanOnce(ctx); err != nil {
			logx.Errorw("scan checkout timeout task failed", logx.Field("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *CheckoutTimeoutScanner) ScanOnce(ctx context.Context) error {
	if s == nil || s.redis == nil || s.checkouts == nil || s.items == nil || s.producer == nil || s.topic == "" {
		return nil
	}
	now := s.now()
	pairs, err := s.redis.ZrangebyscoreWithScoresAndLimitCtx(ctx, biz.CheckoutTimeoutZSetKey, 0, now.Unix(), 0, s.batchSize)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := s.handleCheckout(ctx, pair.Key, now); err != nil {
			logx.Errorw("handle expired checkout timeout task failed",
				logx.Field("err", err),
				logx.Field("pre_order_id", pair.Key))
		}
	}
	return nil
}

func (s *CheckoutTimeoutScanner) handleCheckout(ctx context.Context, preOrderID string, now time.Time) error {
	checkoutRes, err := s.checkouts.FindOne(ctx, preOrderID)
	if err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			_, remErr := s.redis.ZremCtx(ctx, biz.CheckoutTimeoutZSetKey, preOrderID)
			return remErr
		}
		return err
	}
	if servicecheckout.CheckoutStatus(checkoutRes.Status) != servicecheckout.CheckoutStatus_RESERVING {
		_, err := s.redis.ZremCtx(ctx, biz.CheckoutTimeoutZSetKey, preOrderID)
		return err
	}

	items, err := s.items.FindItemsByPreOrder(ctx, preOrderID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("checkout timeout event missing checkout items")
	}
	payload, err := json.Marshal(event.CheckoutTimeout{
		PreOrderId: preOrderID,
		UserId:     int32(checkoutRes.UserId),
		Source:     biz.TimeoutSourceCheckout,
		Items:      checkoutItemsToInventoryItems(items),
	})
	if err != nil {
		return err
	}
	if err := s.producer.PublishWithKey(ctx, s.topic, preOrderID, payload); err != nil {
		return err
	}
	// 更新超时任务的过期时间为当前时间加上重试延迟（30s），避免之后重复消费
	_, err = s.redis.ZaddCtx(ctx, biz.CheckoutTimeoutZSetKey, now.Add(s.retryDelay).Unix(), preOrderID)
	return err
}

func checkoutItemsToInventoryItems(items []*checkoutmodel.CheckoutItems) []*inventory.InventoryReq_Items {
	res := make([]*inventory.InventoryReq_Items, 0, len(items))
	for _, item := range items {
		res = append(res, &inventory.InventoryReq_Items{
			ProductId: int32(item.ProductId),
			Quantity:  int32(item.Quantity),
		})
	}
	return res
}

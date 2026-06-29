package checkout_timeout

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type localCheckoutReleaser struct {
	svcCtx *svc.ServiceContext
}

func (r *localCheckoutReleaser) ReleaseCheckout(ctx context.Context, in *checkout.ReleaseReq) (*checkout.EmptyResp, error) {
	return logic.NewReleaseCheckoutLogic(ctx, r.svcCtx).ReleaseCheckout(in)
}

type redisTimeoutTaskRemover struct {
	rdb *redis.Redis
}

func (r *redisTimeoutTaskRemover) RemoveCheckoutTimeoutTask(ctx context.Context, preOrderID string) error {
	_, err := r.rdb.ZremCtx(ctx, biz.CheckoutTimeoutZSetKey, preOrderID)
	return err
}

func init() {
	consumer.Register("checkout_timeouts", Init)
}

func Init(c config.Config) error {
	kafkaConf, err := c.KafkaMQ.TopicConfig("CheckoutTimeouts")
	if err != nil {
		return err
	}
	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	svcCtx := svc.NewServiceContext(c)
	handler := NewCheckoutTimeoutConsumer(
		&localCheckoutReleaser{svcCtx: svcCtx},
		&redisTimeoutTaskRemover{rdb: svcCtx.RedisClient},
	)

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("checkout timeout consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

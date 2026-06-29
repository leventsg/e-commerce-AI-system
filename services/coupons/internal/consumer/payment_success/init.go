package payment_success

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

type localCouponUser struct {
	svcCtx *svc.ServiceContext
}

func (u *localCouponUser) UseCoupon(ctx context.Context, in *coupons.UseCouponReq) (*coupons.EmptyResp, error) {
	return logic.NewUseCouponLogic(ctx, u.svcCtx).UseCoupon(in)
}

func init() {
	consumer.Register("payment_successes", Init)
}

func Init(c config.Config) error {
	kafkaConf, err := c.KafkaMQ.TopicConfig("PaymentSuccesses")
	if err != nil {
		return err
	}

	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}

	handler := NewPaymentSuccessConsumer(&localCouponUser{
		svcCtx: svc.NewServiceContext(c),
	})

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("payment success coupon consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

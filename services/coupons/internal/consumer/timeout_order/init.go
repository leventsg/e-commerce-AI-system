package timeout_order

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/dal/model/coupons/user_coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type localCouponReleaser struct {
	svcCtx *svc.ServiceContext
}

func (r *localCouponReleaser) ReleaseCoupon(ctx context.Context, in *coupons.ReleaseCouponReq) (*coupons.EmptyResp, error) {
	return logic.NewReleaseCouponLogic(ctx, r.svcCtx).ReleaseCoupon(in)
}

func init() {
	consumer.Register("cancel_orders", Init)
}

func Init(c config.Config) error {
	connModel := sqlx.NewMysql(c.MysqlConfig.DataSource)
	kafkaConf, err := c.KafkaMQ.TopicConfig("CancelOrders")
	if err != nil {
		return err
	}

	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}

	handler := NewTimeoutOrderConsumer(&localCouponReleaser{
		svcCtx: &svc.ServiceContext{
			Config:           c,
			UserCouponsModel: user_coupons.NewUserCouponsModel(connModel),
			Model:            connModel,
		},
	})

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("cancel order coupon consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

package cancel_order

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

type localCheckoutReleaser struct {
	svcCtx *svc.ServiceContext
}

func (r *localCheckoutReleaser) ReleaseCheckout(ctx context.Context, in *checkout.ReleaseReq) (*checkout.EmptyResp, error) {
	return logic.NewReleaseCheckoutLogic(ctx, r.svcCtx).ReleaseCheckout(in)
}

func init() {
	consumer.Register("cancel_orders", Init)
}

func Init(c config.Config) error {
	kafkaConf, err := c.KafkaMQ.TopicConfig("CancelOrders")
	if err != nil {
		return err
	}

	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}

	handler := NewCancelOrderConsumer(&localCheckoutReleaser{
		svcCtx: svc.NewServiceContext(c),
	})

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("cancel order checkout consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

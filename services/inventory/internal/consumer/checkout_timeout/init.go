package checkout_timeout

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/logic"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/zeromicro/go-zero/core/logx"
)

type localInventoryReturnPrer struct {
	svcCtx *svc.ServiceContext
}

func (r *localInventoryReturnPrer) ReturnPreInventory(ctx context.Context, in *inventory.InventoryReq) (*inventory.InventoryResp, error) {
	return logic.NewReturnPreInventoryLogic(ctx, r.svcCtx).ReturnPreInventory(in)
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
	handler := NewCheckoutTimeoutConsumer(&localInventoryReturnPrer{
		svcCtx: svc.NewServiceContext(c),
	})

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("checkout timeout inventory returnpre consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

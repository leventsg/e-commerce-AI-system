package payment_success

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

type localInventoryDecreaser struct {
	svcCtx *svc.ServiceContext
}

func (d *localInventoryDecreaser) DecreaseInventory(ctx context.Context, in *inventory.InventoryReq) (*inventory.InventoryResp, error) {
	return logic.NewDecreaseInventoryLogic(ctx, d.svcCtx).DecreaseInventory(in)
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

	handler := NewPaymentSuccessConsumer(&localInventoryDecreaser{
		svcCtx: svc.NewServiceContext(c),
	})

	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("payment success inventory decrease consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

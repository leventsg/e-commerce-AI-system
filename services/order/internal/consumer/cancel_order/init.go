package cancel_order

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/consumer/registry"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func init() {
	registry.Register("cancel_orders", Init)
}

func Init(c config.Config) error {
	connModel := sqlx.NewMysql(c.MysqlConfig.DataSource)
	model := order.NewOrdersModel(connModel)

	kafkaConf, err := c.KafkaMQ.TopicConfig("CancelOrders")
	if err != nil {
		return err
	}

	consumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	handler := NewCancelOrderConsumer(model, connModel)

	startErr := make(chan error, 1)
	go func() {
		if err := consumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler); err != nil {
			logx.Errorw("cancel order consumer stopped", logx.Field("err", err))
			startErr <- err
		}
	}()
	return nil
}

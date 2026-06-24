package timeout_order

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/consumer"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func init() {
	consumer.Register("timeout_orders", Init)
}

func Init(c config.Config) error {
	connModel := sqlx.NewMysql(c.MysqlConfig.DataSource)
	model := order.NewOrdersModel(connModel)
	orderItemsModel := order.NewOrderItemsModel(connModel)
	outboxModel := order.NewOutboxMessagesModel(connModel)

	kafkaConf, err := c.KafkaMQ.TopicConfig("TimeoutOrders")
	if err != nil {
		return err
	}

	consumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	handler := NewTimeoutOrderConsumer(c, model, orderItemsModel, outboxModel, connModel)

	startErr := make(chan error, 1)
	go func() {
		if err := consumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("timeout order consumer stopped", logx.Field("err", err))
			startErr <- err
		}
	}()
	return nil
}

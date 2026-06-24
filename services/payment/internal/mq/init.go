package mq

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	paymentmodel "github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/config"
	"github.com/streadway/amqp"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	ExchangeName   = "payment-delay-exchange"
	ExchangeKind   = "direct"
	QueueName      = "payment-delay-queue"
	DelayQueueName = "payment-delay-wait-queue"
	RoutingKey     = "payment-delay"
	Delay          = biz.PaymentExpireTime
)

type PaymentDelayMQ struct {
	conn     *amqp.Connection
	config   config.Config
	model    sqlx.SqlConn
	payments paymentmodel.PaymentsModel
	outbox   paymentmodel.PaymentOutboxMessagesModel
}
type PaymentReq struct {
	OrderId string
}

func Init(c config.Config, model sqlx.SqlConn, payments paymentmodel.PaymentsModel, outbox paymentmodel.PaymentOutboxMessagesModel) (*PaymentDelayMQ, error) {
	conn, err := amqp.Dial(c.RabbitMQConfig.Dns())
	if err != nil {
		return nil, err
	}
	// 创建通道
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	// 声明交换机（使用延迟交换机）
	err = ch.ExchangeDeclare(
		ExchangeName, // 交换机名称
		ExchangeKind,
		true,  // 持久化
		false, // 自动删除
		false, // 内部交换机
		false, // 等待确认
		nil,
	)
	if err != nil {
		return nil, err

	}

	// 声明队列
	_, err = ch.QueueDeclare(
		QueueName, // 队列名称
		true,      // 持久化
		false,     // 自动删除
		false,     // 排他性
		false,     // 等待确认
		nil,       // 队列参数
	)
	if err != nil {
		return nil, err

	}

	// 绑定队列到交换机
	if err = ch.QueueBind(
		QueueName,
		RoutingKey,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return nil, err

	}
	_, err = ch.QueueDeclare(
		DelayQueueName,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    ExchangeName,
			"x-dead-letter-routing-key": RoutingKey,
		},
	)
	if err != nil {
		return nil, err
	}

	paymentDelay := &PaymentDelayMQ{
		conn:     conn,
		config:   c,
		model:    model,
		payments: payments,
		outbox:   outbox,
	}
	go paymentDelay.consumer(context.TODO())
	return paymentDelay, nil
}

package mq

import (
	"context"
	"github.com/streadway/amqp"
	"jijizhazha1024/go-mall/services/payment/internal/config"
	"time"
)

const (
	ExchangeName   = "payment-delay-exchange"
	ExchangeKind   = "direct"
	QueueName      = "payment-delay-queue"
	DelayQueueName = "payment-delay-wait-queue"
	RoutingKey     = "payment-delay"
	Delay          = 30 * time.Minute
)

type PaymentDelayMQ struct {
	conn *amqp.Connection
}
type PaymentReq struct {
	OrderId string
}

func Init(c config.Config) (*PaymentDelayMQ, error) {
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
		conn: conn,
	}
	go paymentDelay.consumer(context.TODO())
	return paymentDelay, nil
}

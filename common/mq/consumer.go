package mq

import (
	"context"
	"errors"
	"sync"

	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/queue"
	"github.com/zeromicro/go-zero/core/service"
)

type KafkaConsumer struct {
	mu     sync.Mutex
	config config.KafkaTopicConfig
	queues []queue.MessageQueue
}

func NewKafkaConsumer(c config.KafkaTopicConfig) (Consumer, error) {
	if len(c.Brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}

	return &KafkaConsumer{
		config: c,
	}, nil
}

func (c *KafkaConsumer) Consume(ctx context.Context, topic string, group string, handler Handler, errorHandler Handler) error {
	if c == nil {
		return errors.New("kafka consumer is not initialized")
	}
	if handler == nil {
		return errors.New("kafka consumer handler is nil")
	}

	conf := c.config
	if topic != "" {
		conf.Topic = topic
	}
	if group != "" {
		conf.Group = group
	}
	if conf.Topic == "" {
		return errors.New("kafka topic is empty")
	}
	if conf.Group == "" {
		return errors.New("kafka group is empty")
	}

	q, err := kq.NewQueue(toKqConf(conf), kq.WithHandle(func(ctx context.Context, key, value string) error {
		return handler.Handle(ctx, []byte(value))
	}), kq.WithErrorHandler(func(ctx context.Context, msg kafka.Message, err error) {
		if errorHandler != nil {
			if handleErr := errorHandler.Handle(ctx, msg.Value); handleErr != nil {
				logx.Errorw("kafka error handler failed",
					logx.Field("topic", conf.Topic),
					logx.Field("group", conf.Group),
					logx.Field("err", handleErr))
			}
			return
		}
		logx.Errorw("kafka message consume failed",
			logx.Field("topic", conf.Topic),
			logx.Field("group", conf.Group),
			logx.Field("key", string(msg.Key)),
			logx.Field("value", string(msg.Value)),
			logx.Field("err", err))
	}))
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.queues = append(c.queues, q)
	c.mu.Unlock()

	go func() {
		<-ctx.Done()
		q.Stop()
	}()

	q.Start()
	return nil
}

func (c *KafkaConsumer) Close() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, q := range c.queues {
		q.Stop()
	}
	c.queues = nil
	return nil
}

func toKqConf(c config.KafkaTopicConfig) kq.KqConf {
	return kq.KqConf{
		ServiceConf: service.ServiceConf{
			Name: c.Topic,
		},
		Brokers:       c.Brokers,
		Group:         c.Group,
		Topic:         c.Topic,
		Offset:        c.Offset,
		Conns:         c.Conns,
		Consumers:     c.Consumers,
		Processors:    c.Processors,
		MinBytes:      c.MinBytes,
		MaxBytes:      c.MaxBytes,
		Username:      c.Username,
		Password:      c.Password,
		ForceCommit:   c.ForceCommit,
		CommitInOrder: false,
	}
}

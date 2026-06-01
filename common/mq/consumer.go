package mq

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/config"
)

type KafkaConsumer struct {
}

func NewKafkaConsumer(c config.KafkaConfig) (Consumer, error) {
	return &KafkaConsumer{}, nil
}

func (c *KafkaConsumer) Consume(ctx context.Context, topic string, group string, handler Handler) error {
	return nil
}

func (c *KafkaConsumer) Close() error {
	return nil
}

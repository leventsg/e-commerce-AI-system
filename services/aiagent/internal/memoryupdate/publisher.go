package memoryupdate

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

type KafkaPublisher struct {
	producer mq.Producer
	topic    string
}

func NewKafkaPublisher(c config.Config) *KafkaPublisher {
	kafkaConf, err := c.KafkaMQ.TopicConfig(TopicKeyAiMemoryUpdates)
	if err != nil {
		logx.Errorw("ai memory update publisher disabled",
			logx.Field("component", "memory_update"),
			logx.Field("stage", "publisher_init"),
			logx.Field("err", err))
		return nil
	}
	producer, err := mq.NewKafkaProducer(c.KafkaMQ)
	if err != nil {
		logx.Errorw("ai memory update publisher disabled",
			logx.Field("component", "memory_update"),
			logx.Field("stage", "producer_init"),
			logx.Field("err", err))
		return nil
	}
	return &KafkaPublisher{producer: producer, topic: kafkaConf.Topic}
}

func (p *KafkaPublisher) PublishMemoryUpdate(ctx context.Context, event UpdateEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.producer.PublishWithKey(ctx, p.topic, strconv.FormatUint(event.UserID, 10), raw)
}

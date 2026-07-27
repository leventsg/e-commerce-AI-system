package profileextractor

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
)

const TopicKeyAiUserProfileUpdates = "AiUserProfileUpdates"

type Publisher interface {
	PublishProfileUpdate(ctx context.Context, event UpdateEvent) error
}

type KafkaPublisher struct {
	producer mq.Producer
	topic    string
}

func NewKafkaPublisher(producer mq.Producer, topic string) *KafkaPublisher {
	return &KafkaPublisher{producer: producer, topic: topic}
}

func (p *KafkaPublisher) PublishProfileUpdate(ctx context.Context, event UpdateEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.producer.PublishWithKey(ctx, p.topic, strconv.FormatUint(event.UserID, 10), raw)
}

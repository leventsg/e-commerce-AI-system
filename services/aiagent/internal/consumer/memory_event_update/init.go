package memory_event_update

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

func init() {
	consumer.Register("ai_user_memory_events_updates", Init)
}

func Init(c config.Config) error {
	kafkaConf, err := c.KafkaMQ.TopicConfig(memoryupdate.TopicKeyAiMemoryUpdates)
	if err != nil {
		return err
	}
	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	handler := NewConsumer(svc.NewServiceContext(c).MemoryEventExtractor)
	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group+"-memory-events", handler, nil); err != nil {
			logx.Errorw("ai user memory event update consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

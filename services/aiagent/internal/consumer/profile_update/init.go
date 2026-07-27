package profile_update

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/consumer"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

func init() {
	consumer.Register("ai_user_profile_updates", Init)
}

func Init(c config.Config) error {
	kafkaConf, err := c.KafkaMQ.TopicConfig(profileextractor.TopicKeyAiUserProfileUpdates)
	if err != nil {
		return err
	}
	kafkaConsumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	handler := NewConsumer(svc.NewServiceContext(c).ProfileExtractor)
	go func() {
		if err := kafkaConsumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler, nil); err != nil {
			logx.Errorw("ai user profile update consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

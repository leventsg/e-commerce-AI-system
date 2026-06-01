package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/zeromicro/go-queue/kq"
)

type KafkaProducer struct {
	mu           sync.Mutex
	brokers      []string
	defaultTopic string
	pushers      map[string]*kq.Pusher
}

func NewKafkaProducer(c config.KafkaConfig) (Producer, error) {
	if len(c.Brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}
	if c.Username != "" || c.Password != "" {
		return nil, errors.New("go-queue kq pusher does not support username/password configuration")
	}

	return &KafkaProducer{
		brokers:      append([]string(nil), c.Brokers...),
		defaultTopic: c.Topic,
		pushers:      make(map[string]*kq.Pusher),
	}, nil
}

func (p *KafkaProducer) Publish(ctx context.Context, topic string, message []byte) error {
	if p == nil {
		return errors.New("kafka producer is not initialized")
	}

	if topic == "" {
		topic = p.defaultTopic
	}
	if topic == "" {
		return errors.New("kafka topic is empty")
	}

	return p.getPusher(topic).Push(ctx, string(message))
}

func (p *KafkaProducer) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for topic, pusher := range p.pushers {
		if err := pusher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close kafka pusher topic %s: %w", topic, err))
		}
	}
	p.pushers = make(map[string]*kq.Pusher)

	return errors.Join(errs...)
}

func (p *KafkaProducer) getPusher(topic string) *kq.Pusher {
	p.mu.Lock()
	defer p.mu.Unlock()

	pusher := p.pushers[topic]
	if pusher == nil {
		pusher = kq.NewPusher(p.brokers, topic, kq.WithSyncPush())
		p.pushers[topic] = pusher
	}

	return pusher
}

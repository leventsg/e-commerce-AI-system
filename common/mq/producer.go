package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-queue/kq"
)

type KafkaProducer struct {
	mu           sync.Mutex
	brokers      []string
	defaultTopic string
	pushers      map[pusherKey]*kq.Pusher
}

type PublishOption func(*publishOptions)

type publishOptions struct {
	syncPush               bool
	allowAutoTopicCreation bool
	balancer               kafka.Balancer
	balancerName           string
	chunkSize              int
	flushInterval          time.Duration
}

type pusherKey struct {
	topic                  string
	syncPush               bool
	allowAutoTopicCreation bool
	balancerName           string
	chunkSize              int
	flushInterval          time.Duration
}

func NewKafkaProducer(c config.KafkaConfig) (Producer, error) {
	if len(c.Brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}
	if c.Username != "" || c.Password != "" {
		return nil, errors.New("go-queue kq pusher does not support username/password configuration")
	}

	return &KafkaProducer{
		brokers: append([]string(nil), c.Brokers...),
		pushers: make(map[pusherKey]*kq.Pusher),
	}, nil
}

func (p *KafkaProducer) Publish(ctx context.Context, topic string, message []byte, opts ...PublishOption) error {
	return p.publish(ctx, topic, "", message, opts...)
}

func (p *KafkaProducer) PublishWithKey(ctx context.Context, topic string, key string, message []byte, opts ...PublishOption) error {
	if key == "" {
		return errors.New("kafka message key is empty")
	}
	// 默认使用hash分区策略，保证相同key的消息发送到同一分区，增强消息顺序性
	opts = append(opts, WithHashBalancer())

	return p.publish(ctx, topic, key, message, opts...)
}

func (p *KafkaProducer) publish(ctx context.Context, topic string, key string, message []byte, opts ...PublishOption) error {
	if p == nil {
		return errors.New("kafka producer is not initialized")
	}

	if topic == "" {
		topic = p.defaultTopic
	}
	if topic == "" {
		return errors.New("kafka topic is empty")
	}

	options := defaultPublishOptions()
	for _, opt := range opts {
		opt(&options)
	}

	if key == "" {
		return p.getPusher(topic, options).Push(ctx, string(message))
	}

	return p.getPusher(topic, options).KPush(ctx, key, string(message))
}

func (p *KafkaProducer) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for key, pusher := range p.pushers {
		if err := pusher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close kafka pusher topic %s: %w", key.topic, err))
		}
	}
	p.pushers = make(map[pusherKey]*kq.Pusher)

	return errors.Join(errs...)
}

func (p *KafkaProducer) getPusher(topic string, options publishOptions) *kq.Pusher {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := pusherKey{
		topic:                  topic,
		syncPush:               options.syncPush,
		allowAutoTopicCreation: options.allowAutoTopicCreation,
		balancerName:           options.balancerName,
		chunkSize:              options.chunkSize,
		flushInterval:          options.flushInterval,
	}

	pusher := p.pushers[key]
	if pusher == nil {
		pusher = kq.NewPusher(p.brokers, topic, buildPushOptions(options)...)
		p.pushers[key] = pusher
	}

	return pusher
}

func defaultPublishOptions() publishOptions {
	return publishOptions{
		syncPush:               false,     // 默认异步推送
		allowAutoTopicCreation: true,      // 默认开启自动创建主题
		balancerName:           "default", // 默认使用go-queue的默认分区策略
	}
}

func buildPushOptions(options publishOptions) []kq.PushOption {
	var pushOptions []kq.PushOption

	if options.balancer != nil {
		pushOptions = append(pushOptions, kq.WithBalancer(options.balancer))
	}

	if options.syncPush {
		pushOptions = append(pushOptions, kq.WithSyncPush())
	}
	if options.allowAutoTopicCreation {
		pushOptions = append(pushOptions, kq.WithAllowAutoTopicCreation())
	}
	if options.chunkSize > 0 {
		pushOptions = append(pushOptions, kq.WithChunkSize(options.chunkSize))
	}
	if options.flushInterval > 0 {
		pushOptions = append(pushOptions, kq.WithFlushInterval(options.flushInterval))
	}

	return pushOptions
}

func WithSyncPublish() PublishOption {
	return func(options *publishOptions) {
		options.syncPush = true
	}
}

func WithAsyncPublish() PublishOption {
	return func(options *publishOptions) {
		options.syncPush = false
	}
}

func WithAllowAutoTopicCreation() PublishOption {
	return func(options *publishOptions) {
		options.allowAutoTopicCreation = true
	}
}

func WithoutAutoTopicCreation() PublishOption {
	return func(options *publishOptions) {
		options.allowAutoTopicCreation = false
	}
}

func WithHashBalancer() PublishOption {
	return func(options *publishOptions) {
		options.balancer = &kafka.Hash{}
		options.balancerName = "hash"
	}
}

func WithMurmur2Balancer() PublishOption {
	return func(options *publishOptions) {
		options.balancer = &kafka.Murmur2Balancer{Consistent: true}
		options.balancerName = "murmur2"
	}
}

func WithCRC32Balancer() PublishOption {
	return func(options *publishOptions) {
		options.balancer = kafka.CRC32Balancer{Consistent: true}
		options.balancerName = "crc32"
	}
}

func WithRoundRobinBalancer() PublishOption {
	return func(options *publishOptions) {
		options.balancer = &kafka.RoundRobin{}
		options.balancerName = "round_robin"
	}
}

func WithLeastBytesBalancer() PublishOption {
	return func(options *publishOptions) {
		options.balancer = &kafka.LeastBytes{}
		options.balancerName = "least_bytes"
	}
}

func WithCustomBalancer(name string, balancer kafka.Balancer) PublishOption {
	return func(options *publishOptions) {
		if name == "" {
			name = fmt.Sprintf("%T", balancer)
		}
		options.balancer = balancer
		options.balancerName = name
	}
}

func WithChunkSize(size int) PublishOption {
	return func(options *publishOptions) {
		options.chunkSize = size
	}
}

func WithFlushInterval(interval time.Duration) PublishOption {
	return func(options *publishOptions) {
		options.flushInterval = interval
	}
}

package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/zeromicro/go-zero/core/logx"
)

type Message struct {
	Id            uint64
	Topic         string
	MessageKey    string
	Payload       string
	RetryCount    int64
	MaxRetryCount int64
}

type MessageStore interface {
	ClaimPending(ctx context.Context, limit int, lockTTL time.Duration) ([]*Message, error)
	MarkSent(ctx context.Context, id uint64) error
	MarkRetry(ctx context.Context, id uint64, lastError string, nextRetryAt time.Time) error
	MarkFailed(ctx context.Context, id uint64, lastError string) error
}

type Config struct {
	BatchSize    int
	ScanInterval time.Duration
	LockTTL      time.Duration
	RetryBase    time.Duration
}

type Dispatcher struct {
	config   Config
	store    MessageStore
	producer mq.Producer
}

func NewDispatcher(config Config, store MessageStore, producer mq.Producer) *Dispatcher {
	if config.BatchSize <= 0 {
		config.BatchSize = 50
	}
	if config.ScanInterval <= 0 {
		config.ScanInterval = 2 * time.Second
	}
	if config.LockTTL <= 0 {
		config.LockTTL = 30 * time.Second
	}
	if config.RetryBase <= 0 {
		config.RetryBase = time.Second
	}
	return &Dispatcher{
		config:   config,
		store:    store,
		producer: producer,
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	if d == nil {
		return
	}
	ticker := time.NewTicker(d.config.ScanInterval)
	defer ticker.Stop()

	for {
		if err := d.DispatchOnce(ctx); err != nil {
			logx.Errorw("outbox dispatch failed", logx.Field("err", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) DispatchOnce(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if d.store == nil {
		return errors.New("outbox message store is nil")
	}
	if d.producer == nil {
		return errors.New("outbox producer is nil")
	}

	messages, err := d.store.ClaimPending(ctx, d.config.BatchSize, d.config.LockTTL)
	if err != nil {
		return err
	}
	for _, message := range messages {
		d.dispatchMessage(ctx, message)
	}
	return nil
}

func (d *Dispatcher) dispatchMessage(ctx context.Context, message *Message) {
	if message == nil {
		return
	}

	var err error
	if message.MessageKey == "" {
		err = d.producer.Publish(ctx, message.Topic, []byte(message.Payload))
	} else {
		err = d.producer.PublishWithKey(ctx, message.Topic, message.MessageKey, []byte(message.Payload))
	}

	if err == nil {
		if markErr := d.store.MarkSent(ctx, message.Id); markErr != nil {
			logx.Errorw("mark outbox message sent failed", logx.Field("err", markErr), logx.Field("id", message.Id))
		}
		return
	}

	if message.RetryCount+1 >= message.MaxRetryCount {
		if markErr := d.store.MarkFailed(ctx, message.Id, err.Error()); markErr != nil {
			logx.Errorw("mark outbox message failed failed", logx.Field("err", markErr), logx.Field("id", message.Id))
		}
		return
	}

	nextRetryAt := time.Now().Add(d.retryDelay(message.RetryCount + 1))
	if markErr := d.store.MarkRetry(ctx, message.Id, err.Error(), nextRetryAt); markErr != nil {
		logx.Errorw("mark outbox message retry failed", logx.Field("err", markErr), logx.Field("id", message.Id))
	}
}

func (d *Dispatcher) retryDelay(nextRetryCount int64) time.Duration {
	if nextRetryCount <= 1 {
		return d.config.RetryBase
	}
	delay := d.config.RetryBase
	for i := int64(1); i < nextRetryCount; i++ {
		delay *= 2
		if delay > time.Minute {
			return time.Minute
		}
	}
	return delay
}

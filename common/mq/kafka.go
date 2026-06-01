package mq

import "context"

type Producer interface {
	Publish(ctx context.Context, topic string, message []byte) error
	Close() error
}

type Consumer interface {
	Consume(ctx context.Context, topic string, group string, handler Handler) error
	Close() error
}

type Handler interface {
	Handle(ctx context.Context, msg []byte) error
}

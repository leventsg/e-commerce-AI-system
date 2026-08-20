package memory_event_update

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryeventextractor"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
	"github.com/zeromicro/go-zero/core/logx"
)

type Extractor interface {
	Handle(ctx context.Context, event memoryupdate.UpdateEvent) error
}

type Consumer struct {
	extractor Extractor
}

func NewConsumer(extractor Extractor) *Consumer {
	return &Consumer{extractor: extractor}
}

func (c *Consumer) Handle(ctx context.Context, msg []byte) error {
	if c == nil || c.extractor == nil {
		return errors.New("memory event extractor is nil")
	}
	var event memoryupdate.UpdateEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		return err
	}
	if err := c.extractor.Handle(ctx, event); err != nil {
		if errors.Is(err, memoryeventextractor.ErrRejectedCandidate) {
			logx.Errorw("ai user memory event candidate rejected",
				logx.Field("component", "memory_event_extractor"),
				logx.Field("stage", "consume_update_event"),
				logx.Field("event_id", event.EventID),
				logx.Field("user_id", event.UserID),
				logx.Field("err", err))
			return nil
		}
		return err
	}
	return nil
}

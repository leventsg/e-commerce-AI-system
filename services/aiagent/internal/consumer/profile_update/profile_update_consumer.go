package profile_update

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/profileextractor"
	"github.com/zeromicro/go-zero/core/logx"
)

type Extractor interface {
	Handle(ctx context.Context, event profileextractor.UpdateEvent) error
}

type Consumer struct {
	extractor Extractor
}

func NewConsumer(extractor Extractor) *Consumer {
	return &Consumer{extractor: extractor}
}

func (c *Consumer) Handle(ctx context.Context, msg []byte) error {
	if c == nil || c.extractor == nil {
		return errors.New("profile extractor is nil")
	}
	var event profileextractor.UpdateEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		return err
	}
	if err := c.extractor.Handle(ctx, event); err != nil {
		if errors.Is(err, profileextractor.ErrRejectedCandidate) {
			logx.Errorw("ai user profile candidate rejected",
				logx.Field("component", "profile_extractor"),
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

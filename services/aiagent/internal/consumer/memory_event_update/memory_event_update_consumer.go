package memory_event_update

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryeventextractor"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
	"github.com/zeromicro/go-zero/core/logx"
)

type Extractor interface {
	Handle(ctx context.Context, event memoryupdate.UpdateEvent) error
}

type Consumer struct {
	extractor   Extractor
	observation *eino.Observation
}

func NewConsumer(extractor Extractor, observations ...*eino.Observation) *Consumer {
	c := &Consumer{extractor: extractor}
	if len(observations) > 0 {
		c.observation = observations[0]
	}
	return c
}

func (c *Consumer) Handle(ctx context.Context, msg []byte) error {
	if c == nil || c.extractor == nil {
		return errors.New("memory event extractor is nil")
	}
	var event memoryupdate.UpdateEvent
	if err := json.Unmarshal(msg, &event); err != nil {
		return err
	}
	ctx, end := c.observation.Start(obs.Detached(ctx), "memory.memory_event_update", event.UserID, event.ConversationID, "")
	defer end("")
	ctx = obs.Correlate(ctx, map[string]string{"source_trace_id": event.SourceTraceID, "event_id": event.EventID})
	if err := c.extractor.Handle(ctx, event); err != nil {
		obs.Result(ctx, "failed", "")
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

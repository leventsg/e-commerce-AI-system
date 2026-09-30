package logic

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
)

// observedSender observes the events delivered by the existing persistence/SSE path.
type observedSender struct {
	agentEventSender
	ctx    context.Context
	output string
	status string
}

func (s *observedSender) Send(event *aiagent.AgentEvent) error {
	if event != nil {
		switch event.Type {
		case domain.EventError:
			s.status = "failed"
		case domain.EventConfirmationRequired:
			if s.status != "failed" {
				s.status = "pending"
			}
		case domain.EventToolResult:
			if event.Status == "failed" {
				s.status = "failed"
			}
		case domain.EventAssistantMessage:
			s.output = event.Content
		}
		if event.Type == domain.EventConfirmationRequired {
			obs.Event(s.ctx, "confirmation.wait", "pending", map[string]string{"confirmation_id": event.ConfirmationId, "tool": event.Tool})
		}
	}
	err := s.agentEventSender.Send(event)
	if err != nil {
		s.status = "failed"
	}
	return err
}

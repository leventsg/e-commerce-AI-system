package memoryeventextractor

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
)

const (
	EventTypeMilestone = "milestone"
	EventTypeEvent     = "event"
	EventTypeNone      = "none"

	eventMinConfidence = 0.75
)

var (
	ErrInvalidEvent      = errors.New("invalid memory event update event")
	ErrRejectedCandidate = errors.New("memory event candidate rejected")

	sensitiveEventPattern = regexp.MustCompile(`(?i)(user_id|token|auth|password|passwd|secret|api[_-]?key|cookie|支付密码|验证码|身份证|银行卡|完整地址|详细地址|收货地址)`)
)

type UpdateEvent = memoryupdate.UpdateEvent

type ExtractRequest struct {
	Event    UpdateEvent
	Messages []*aimessages.AiMessages
}

type Candidate struct {
	Events []EventCandidate `json:"events"`
}

type EventCandidate struct {
	Type               string   `json:"type"`
	EventDate          string   `json:"event_date"`
	Summary            string   `json:"summary"`
	Keywords           []string `json:"keywords"`
	EvidenceMessageIDs []string `json:"evidence_message_ids"`
	Confidence         float64  `json:"confidence"`
	Reason             string   `json:"reason"`
}

func ParseCandidate(output string) (Candidate, error) {
	if output == "" || !strings.HasPrefix(strings.TrimLeft(output, " \t\r\n"), "{") {
		return Candidate{}, ErrRejectedCandidate
	}
	var candidate Candidate
	if err := json.Unmarshal([]byte(output), &candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

type Model interface {
	// Extract returns long-term timeline events from compressed conversation messages.
	Extract(ctx context.Context, req ExtractRequest) (Candidate, error)
}

type Extractor struct {
	messages aimessages.AiMessagesModel
	events   *aimemory.SQLEventStore
	model    Model
}

func NewExtractor(messages aimessages.AiMessagesModel, events *aimemory.SQLEventStore, model Model) *Extractor {
	return &Extractor{messages: messages, events: events, model: model}
}

func (e *Extractor) Handle(ctx context.Context, event UpdateEvent) error {
	if e == nil || e.messages == nil || e.events == nil || e.model == nil ||
		event.EventID == "" || event.UserID == 0 || event.ConversationID == "" || len(event.MessageIDs) == 0 {
		return ErrInvalidEvent
	}
	messages, err := e.messages.FindMessagesByIDs(ctx, event.UserID, event.ConversationID, event.MessageIDs)
	if err != nil {
		return err
	}
	candidate, err := e.model.Extract(ctx, ExtractRequest{Event: event, Messages: messages})
	if err != nil {
		return err
	}
	events, err := buildMemoryEvents(event, messages, candidate)
	if err != nil || len(events) == 0 {
		return err
	}
	return e.events.SaveUserMemoryEvents(ctx, event.UserID, events)
}

func buildMemoryEvents(event UpdateEvent, messages []*aimessages.AiMessages, candidate Candidate) ([]aimemory.Event, error) {
	allowedEvidence := make(map[string]bool, len(messages))
	for _, message := range messages {
		if message != nil && message.UserId == event.UserID && message.ConversationId == event.ConversationID {
			allowedEvidence[message.MsgId] = true
		}
	}
	events := make([]aimemory.Event, 0, len(candidate.Events))
	for _, item := range candidate.Events {
		if item.Type == EventTypeNone {
			continue
		}
		if !validEventCandidate(allowedEvidence, item) {
			return nil, ErrRejectedCandidate
		}
		events = append(events, aimemory.Event{
			UserID:    event.UserID,
			Type:      item.Type,
			EventDate: item.EventDate,
			Summary:   strings.TrimSpace(item.Summary),
			Keywords:  compactEventKeywords(item.Keywords),
		})
	}
	return events, nil
}

func validEventCandidate(allowedEvidence map[string]bool, item EventCandidate) bool {
	if item.Type != EventTypeMilestone && item.Type != EventTypeEvent {
		return false
	}
	if item.Confidence < eventMinConfidence || strings.TrimSpace(item.Summary) == "" || len([]rune(item.Summary)) > 512 {
		return false
	}
	if _, err := time.Parse("2006-01-02", item.EventDate); err != nil {
		return false
	}
	if len(item.EvidenceMessageIDs) == 0 {
		return false
	}
	for _, id := range item.EvidenceMessageIDs {
		if !allowedEvidence[id] {
			return false
		}
	}
	if sensitiveEventPattern.MatchString(item.Summary) {
		return false
	}
	for _, keyword := range item.Keywords {
		if sensitiveEventPattern.MatchString(keyword) {
			return false
		}
	}
	return true
}

func compactEventKeywords(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

package memoryeventextractor

import (
	"testing"
	"time"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memoryupdate"
)

func TestBuildMemoryEventsAcceptsValidCandidate(t *testing.T) {
	event := memoryupdate.UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}}
	messages := []*aimessages.AiMessages{{
		MsgId: "msg-1", UserId: 42, ConversationId: "conv-1", Role: domain.ContextRoleUser,
		Content: "以后推荐手机优先轻薄款", CreatedAt: time.Now(),
	}}

	events, err := buildMemoryEvents(event, messages, Candidate{Events: []EventCandidate{{
		Type:               EventTypeEvent,
		EventDate:          "2026-08-14",
		Summary:            "用户表达未来推荐手机优先轻薄款。",
		Keywords:           []string{"手机", "轻薄"},
		EvidenceMessageIDs: []string{"msg-1"},
		Confidence:         0.91,
	}}})
	if err != nil {
		t.Fatalf("buildMemoryEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].UserID != 42 || events[0].Summary == "" || len(events[0].Keywords) != 2 {
		t.Fatalf("events = %+v", events)
	}
}

func TestBuildMemoryEventsRejectsCrossConversationEvidence(t *testing.T) {
	event := memoryupdate.UpdateEvent{EventID: "evt-1", UserID: 42, ConversationID: "conv-1", MessageIDs: []string{"msg-1"}}
	messages := []*aimessages.AiMessages{{MsgId: "msg-1", UserId: 42, ConversationId: "conv-2"}}

	_, err := buildMemoryEvents(event, messages, Candidate{Events: []EventCandidate{{
		Type:               EventTypeEvent,
		EventDate:          "2026-08-14",
		Summary:            "用户表达长期偏好。",
		EvidenceMessageIDs: []string{"msg-1"},
		Confidence:         0.91,
	}}})
	if err != ErrRejectedCandidate {
		t.Fatalf("buildMemoryEvents() error = %v, want ErrRejectedCandidate", err)
	}
}

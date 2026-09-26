//go:build ai_business_agents

package confirmation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	rows "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// The store substitutes persistence only. Manager ownership, expiry and terminal
// state checks run unchanged; unexpected generated-model calls panic via nil.
type businessConfirmationStore struct {
	rows.AiConfirmationsModel
	row *rows.AiConfirmations
}

func (s *businessConfirmationStore) Insert(_ context.Context, row *rows.AiConfirmations) (sql.Result, error) {
	copy := *row
	s.row = &copy
	return nil, nil
}
func (s *businessConfirmationStore) FindOneUncached(_ context.Context, id string) (*rows.AiConfirmations, error) {
	if s.row == nil || s.row.Id != id {
		return nil, rows.ErrNotFound
	}
	copy := *s.row
	return &copy, nil
}
func (s *businessConfirmationStore) ResolvePending(_ context.Context, id string, user uint64, next string, now time.Time) (bool, error) {
	if s.row.Id != id || s.row.UserId != user || s.row.Status != "pending" || !s.row.ExpiresAt.After(now) {
		return false, nil
	}
	s.row.Status = next
	return true, nil
}
func (s *businessConfirmationStore) ExpirePending(_ context.Context, id string, user uint64, now time.Time) (bool, error) {
	if s.row.Id != id || s.row.UserId != user || s.row.Status != "pending" || s.row.ExpiresAt.After(now) {
		return false, nil
	}
	s.row.Status = "expired"
	return true, nil
}
func (s *businessConfirmationStore) CompleteApproved(_ context.Context, id string, user uint64, next string, now time.Time) (bool, error) {
	if s.row.Id != id || s.row.UserId != user || s.row.Status != "approved" {
		return false, nil
	}
	s.row.Status = next
	s.row.ExecutedAt = sql.NullTime{Time: now, Valid: true}
	return true, nil
}
func (s *businessConfirmationStore) BindResumeTarget(_ context.Context, id string, user uint64, run, checkpoint, interrupt string) (bool, error) {
	if s.row.Id != id || s.row.UserId != user || s.row.Status != "pending" {
		return false, nil
	}
	s.row.RunId = run
	s.row.CheckpointId = checkpoint
	s.row.InterruptId = interrupt
	return true, nil
}

type businessConfirmationMetadata struct{}

func (businessConfirmationMetadata) Metadata(name string) (domain.Metadata, error) {
	return domain.Metadata{Name: name, Risk: "high", RequireConfirmation: true, WriteOperation: true}, nil
}
func TestBusinessConfirmationStateGuards(t *testing.T) {
	for _, tool := range []string{"order_create", "order_cancel", "cart_delete"} {
		for _, scenario := range []string{"executed", "failed", "rejected", "expired", "cross_user", "cross_conversation"} {
			t.Run(tool+"/"+scenario, func(t *testing.T) {
				ctx := context.Background()
				now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
				store := &businessConfirmationStore{}
				manager := NewManager(store, businessConfirmationMetadata{}, nil, WithClock(func() time.Time { return now }), WithIDGenerator(func() string { return "confirmation-1" }), WithConfirmationTTL(time.Minute))
				created, err := manager.Create(ctx, CreateRequest{UserID: 7, ConversationID: "conversation-1", ToolName: tool, Summary: "confirm operation", Arguments: map[string]any{"user_id": 999, "token": "untrusted", "product_id": 42}, RunID: "run-1", CheckpointID: "checkpoint-1"})
				if err != nil {
					t.Fatal(err)
				}
				if created.Status != StatusPending || created.UserID != 7 {
					t.Fatalf("invalid creation: %+v", created)
				}
				if _, exists := created.Arguments["user_id"]; exists {
					t.Fatal("untrusted user_id persisted")
				}
				if _, exists := created.Arguments["token"]; exists {
					t.Fatal("untrusted token persisted")
				}
				bound, err := manager.BindResumeTarget(ctx, ResumeTargetRequest{UserID: 7, ConversationID: "conversation-1", ConfirmationID: created.ID, RunID: "run-1", CheckpointID: "checkpoint-1", InterruptID: "interrupt-1"})
				if err != nil || bound.InterruptID != "interrupt-1" {
					t.Fatalf("binding: %+v %v", bound, err)
				}
				decision := DecisionRequest{UserID: 7, ConversationID: "conversation-1", ConfirmationID: created.ID, Approved: true}
				completion := CompletionRequest{UserID: 7, ConversationID: "conversation-1", ConfirmationID: created.ID}
				switch scenario {
				case "expired":
					now = now.Add(time.Minute)
					if _, err := manager.Decide(ctx, decision); !errors.Is(err, ErrConfirmationExpired) {
						t.Fatalf("expiry: %v", err)
					}
					if store.row.Status != StatusExpired {
						t.Fatal("expiry not persisted")
					}
				case "cross_user", "cross_conversation":
					if scenario == "cross_user" {
						decision.UserID = 8
						completion.UserID = 8
					} else {
						decision.ConversationID = "conversation-2"
						completion.ConversationID = "conversation-2"
					}
					if _, err := manager.Decide(ctx, decision); !errors.Is(err, ErrConfirmationForbidden) {
						t.Fatalf("ownership: %v", err)
					}
					if _, err := manager.MarkExecuted(ctx, completion); !errors.Is(err, ErrConfirmationForbidden) {
						t.Fatalf("completion ownership: %v", err)
					}
					if store.row.Status != StatusPending {
						t.Fatal("foreign request changed state")
					}
					return
				case "rejected":
					decision.Approved = false
					rejected, err := manager.Decide(ctx, decision)
					if err != nil || rejected.Status != StatusRejected {
						t.Fatalf("reject: %+v %v", rejected, err)
					}
					decision.Approved = true
					if _, err := manager.Decide(ctx, decision); !errors.Is(err, ErrConfirmationRejected) {
						t.Fatalf("rejected approval: %v", err)
					}
				default:
					if _, err := manager.MarkExecuted(ctx, completion); !errors.Is(err, ErrConfirmationAlreadyProcessed) {
						t.Fatalf("completed pending action: %v", err)
					}
					approved, err := manager.Decide(ctx, decision)
					if err != nil || approved.Status != StatusApproved {
						t.Fatalf("approve: %+v %v", approved, err)
					}
					if _, err := manager.Decide(ctx, decision); !errors.Is(err, ErrConfirmationAlreadyProcessed) {
						t.Fatalf("duplicate approve: %v", err)
					}
					var result *domain.Confirmation
					if scenario == "executed" {
						result, err = manager.MarkExecuted(ctx, completion)
					} else {
						result, err = manager.MarkFailed(ctx, completion)
					}
					if err != nil || result.Status != scenario || result.ExecutedAt == nil {
						t.Fatalf("complete: %+v %v", result, err)
					}
					if _, err := manager.Decide(ctx, decision); !errors.Is(err, ErrConfirmationAlreadyProcessed) {
						t.Fatalf("terminal replay: %v", err)
					}
				}
				if _, err := manager.MarkExecuted(ctx, completion); !errors.Is(err, ErrConfirmationAlreadyProcessed) {
					t.Fatalf("invalid completion: %v", err)
				}
			})
		}
	}
}

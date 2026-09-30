package eino

import (
	"context"
	"database/sql"
	rows "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	"sync"
	"time"
)

// evaluationConfirmations substitutes persistence only; the production Manager
// still enforces ownership, expiry and atomic terminal state transitions.
type evaluationConfirmations struct {
	rows.AiConfirmationsModel
	mu   sync.Mutex
	data map[string]*rows.AiConfirmations
}

func (s *evaluationConfirmations) Insert(_ context.Context, v *rows.AiConfirmations) (sql.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *v
	s.data[v.Id] = &copy
	return nil, nil
}
func (s *evaluationConfirmations) FindOneUncached(_ context.Context, id string) (*rows.AiConfirmations, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.data[id]
	if v == nil {
		return nil, rows.ErrNotFound
	}
	copy := *v
	return &copy, nil
}
func (s *evaluationConfirmations) ResolvePending(_ context.Context, id string, u uint64, next string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.data[id]
	if v == nil || v.UserId != u || v.Status != "pending" || !v.ExpiresAt.After(now) {
		return false, nil
	}
	v.Status = next
	return true, nil
}
func (s *evaluationConfirmations) ExpirePending(_ context.Context, id string, u uint64, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.data[id]
	if v == nil || v.UserId != u || v.Status != "pending" || v.ExpiresAt.After(now) {
		return false, nil
	}
	v.Status = "expired"
	return true, nil
}
func (s *evaluationConfirmations) CompleteApproved(_ context.Context, id string, u uint64, next string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.data[id]
	if v == nil || v.UserId != u || v.Status != "approved" {
		return false, nil
	}
	v.Status = next
	v.ExecutedAt = sql.NullTime{Time: now, Valid: true}
	return true, nil
}
func (s *evaluationConfirmations) BindResumeTarget(_ context.Context, id string, u uint64, run, checkpoint, interrupt string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.data[id]
	if v == nil || v.UserId != u || v.Status != "pending" {
		return false, nil
	}
	v.RunId = run
	v.CheckpointId = checkpoint
	v.InterruptId = interrupt
	return true, nil
}

package confirmation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aiconfirmations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/confirmations"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

var fixedNow = time.Date(2026, 7, 17, 12, 0, 0, 0, time.Local)

func TestManagerCreateStoresSanitizedPendingConfirmation(t *testing.T) {
	model := newFakeModel()
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})

	created, err := manager.Create(context.Background(), CreateRequest{
		UserID:         42,
		ConversationID: "conv-1",
		ToolName:       domain.ToolOrderCancel,
		Summary:        "确认取消订单 order-1？",
		Arguments: map[string]any{
			"order_id":      "order-1",
			"userId":        999,
			"Authorization": "Bearer secret",
			"access-token":  "access-secret",
			"refreshToken":  "refresh-secret",
			"cookie":        "session=secret",
			"JWT":           "jwt-secret",
			"nested": map[string]any{
				"token": "secret",
				"keep":  "value",
			},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "confirm-fixed" || created.Status != StatusPending {
		t.Fatalf("created confirmation = %#v", created)
	}
	if !created.ExpiresAt.Equal(fixedNow.Add(defaultConfirmationTTL)) {
		t.Fatalf("ExpiresAt = %v", created.ExpiresAt)
	}
	for _, key := range []string{"userId", "Authorization", "access-token", "refreshToken", "cookie", "JWT"} {
		if _, ok := created.Arguments[key]; ok {
			t.Fatalf("sensitive key %q retained: %#v", key, created.Arguments)
		}
	}
	nested := created.Arguments["nested"].(map[string]any)
	if _, ok := nested["token"]; ok || nested["keep"] != "value" {
		t.Fatalf("nested arguments = %#v", nested)
	}
	row := model.row("confirm-fixed")
	if row == nil || row.Status != StatusPending || row.UserId != 42 || row.ToolName != domain.ToolOrderCancel {
		t.Fatalf("stored row = %#v", row)
	}
	if strings.Contains(strings.ToLower(row.Arguments), "secret") || strings.Contains(row.Arguments, "userId") {
		t.Fatalf("stored sensitive arguments: %s", row.Arguments)
	}
}

func TestManagerBindResumeTargetStoresCheckpointInterruptMapping(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})

	updated, err := manager.BindResumeTarget(context.Background(), ResumeTargetRequest{
		UserID:         42,
		ConversationID: "conv-1",
		ConfirmationID: "confirm-1",
		RunID:          "run-1",
		CheckpointID:   "checkpoint-1",
		InterruptID:    "interrupt-1",
	})
	if err != nil {
		t.Fatalf("BindResumeTarget: %v", err)
	}
	if updated.RunID != "run-1" || updated.CheckpointID != "checkpoint-1" || updated.InterruptID != "interrupt-1" {
		t.Fatalf("updated confirmation = %#v", updated)
	}
	row := model.row("confirm-1")
	if row.RunId != "run-1" || row.CheckpointId != "checkpoint-1" || row.InterruptId != "interrupt-1" {
		t.Fatalf("stored row = %#v", row)
	}
}

func TestManagerCreateUsesConfirmIDPrefix(t *testing.T) {
	model := newFakeModel()
	manager := NewManager(model, highRiskRegistry(), nil, WithClock(func() time.Time { return fixedNow }))

	created, err := manager.Create(context.Background(), CreateRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCancel,
		Summary: "确认取消订单？", Arguments: map[string]any{"order_id": "order-1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(created.ID, "confirm_") {
		t.Fatalf("confirmation ID = %q, want confirm_ prefix", created.ID)
	}
}

func TestManagerCreateTreatsNonNilInsertResultAsCommitted(t *testing.T) {
	model := newFakeModel()
	model.insertResult = fakeInsertResult{}
	model.insertErr = errors.New("cache invalidation failed")
	manager := newTestManager(model, highRiskRegistry(), nil)

	created, err := manager.Create(context.Background(), CreateRequest{
		UserID: 42, ConversationID: "conv-1", ToolName: domain.ToolOrderCancel,
		Summary: "确认取消订单？", Arguments: map[string]any{"order_id": "order-1"},
	})
	if err != nil || created == nil {
		t.Fatalf("Create result=%#v err=%v, want committed confirmation", created, err)
	}
}

func TestManagerCreateRejectsToolWithoutHighRiskConfirmationMetadata(t *testing.T) {
	tests := []struct {
		name     string
		registry *fakeRegistry
		toolName string
	}{
		{name: "missing", registry: highRiskRegistry(), toolName: "missing.tool"},
		{name: "low risk", registry: registryWith(domain.Metadata{Name: domain.ToolCartAdd, Risk: domain.RiskLow, WriteOperation: true}), toolName: domain.ToolCartAdd},
		{name: "no confirmation", registry: registryWith(domain.Metadata{Name: domain.ToolOrderCancel, Risk: domain.RiskHigh, WriteOperation: true}), toolName: domain.ToolOrderCancel},
		{name: "not write", registry: registryWith(domain.Metadata{Name: domain.ToolOrderCancel, Risk: domain.RiskHigh, RequireConfirmation: true}), toolName: domain.ToolOrderCancel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newFakeModel()
			manager := newTestManager(model, tt.registry, &fakeLocker{})
			_, err := manager.Create(context.Background(), CreateRequest{
				UserID: 42, ConversationID: "conv-1", ToolName: tt.toolName,
				Summary: "确认操作？", Arguments: map[string]any{"id": 1},
			})
			if !errors.Is(err, ErrConfirmationToolNotAllowed) {
				t.Fatalf("Create error = %v", err)
			}
			if model.insertCalls != 0 {
				t.Fatal("invalid tool confirmation was inserted")
			}
		})
	}
}

func TestManagerDecideApprovesAndRejectsPendingConfirmation(t *testing.T) {
	tests := []struct {
		name     string
		approved bool
		status   string
	}{
		{name: "approve", approved: true, status: StatusApproved},
		{name: "reject", approved: false, status: StatusRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
			lock := &fakeLock{}
			locker := &fakeLocker{lock: lock}
			manager := newTestManager(model, highRiskRegistry(), locker)

			result, err := manager.Decide(context.Background(), DecisionRequest{
				UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: tt.approved,
			})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if result.Status != tt.status || model.row("confirm-1").Status != tt.status {
				t.Fatalf("result=%#v row=%#v", result, model.row("confirm-1"))
			}
			if locker.lastKey != "ai:confirmation:lock:confirm-1" || locker.lastTTL != defaultLockTTL {
				t.Fatalf("lock key=%q ttl=%v", locker.lastKey, locker.lastTTL)
			}
			if lock.releaseCalls != 1 {
				t.Fatalf("release calls = %d", lock.releaseCalls)
			}
		})
	}
}

func TestManagerLockContentionSkipsMySQL(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{acquired: boolPtr(false)})

	_, err := manager.Decide(context.Background(), DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if !errors.Is(err, ErrConfirmationBusy) {
		t.Fatalf("Decide error = %v", err)
	}
	if model.readCalls != 0 || model.resolveCalls != 0 {
		t.Fatalf("MySQL touched during lock contention: reads=%d resolves=%d", model.readCalls, model.resolveCalls)
	}
}

func TestManagerRedisFailureFallsBackToMySQLCAS(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{err: errors.New("redis unavailable")})

	result, err := manager.Decide(context.Background(), DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if err != nil {
		t.Fatalf("Decide fallback: %v", err)
	}
	if result.Status != StatusApproved || model.resolveCalls != 1 {
		t.Fatalf("fallback result=%#v resolveCalls=%d", result, model.resolveCalls)
	}
}

func TestManagerReleaseFailureDoesNotOverrideCommittedState(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	lock := &fakeLock{err: errors.New("release failed")}
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{lock: lock})

	result, err := manager.Decide(context.Background(), DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if err != nil || result.Status != StatusApproved {
		t.Fatalf("Decide result=%#v err=%v", result, err)
	}
}

func TestManagerReleaseUsesDetachedBoundedContext(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	lock := &fakeLock{honorContext: true}
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{lock: lock})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = manager.Decide(ctx, DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if lock.releaseCalls != 1 || lock.releaseContextErr != nil || !lock.releaseHadDeadline {
		t.Fatalf("release calls=%d ctxErr=%v deadline=%v", lock.releaseCalls, lock.releaseContextErr, lock.releaseHadDeadline)
	}
}

func TestManagerDecideExpiresPendingConfirmation(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow)
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})

	_, err := manager.Decide(context.Background(), DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if !errors.Is(err, ErrConfirmationExpired) {
		t.Fatalf("Decide error = %v", err)
	}
	if model.row("confirm-1").Status != StatusExpired || model.expireCalls != 1 || model.resolveCalls != 0 {
		t.Fatalf("expired row=%#v expire=%d resolve=%d", model.row("confirm-1"), model.expireCalls, model.resolveCalls)
	}
}

func TestManagerExpireCASLossReturnsProcessedState(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow)
	model.expireWinnerStatus = StatusApproved
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})

	_, err := manager.Decide(context.Background(), DecisionRequest{
		UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true,
	})
	if !errors.Is(err, ErrConfirmationAlreadyProcessed) {
		t.Fatalf("Decide error = %v", err)
	}
	if model.row("confirm-1").Status != StatusApproved {
		t.Fatalf("row = %#v", model.row("confirm-1"))
	}
}

func TestManagerRejectsCrossUserAndConversation(t *testing.T) {
	tests := []DecisionRequest{
		{UserID: 99, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true},
		{UserID: 42, ConversationID: "conv-other", ConfirmationID: "confirm-1", Approved: true},
	}
	for _, req := range tests {
		model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
		manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})
		_, err := manager.Decide(context.Background(), req)
		if !errors.Is(err, ErrConfirmationForbidden) {
			t.Fatalf("Decide(%#v) error = %v", req, err)
		}
		if model.resolveCalls != 0 || model.row("confirm-1").Status != StatusPending {
			t.Fatalf("forbidden request mutated row: %#v", model.row("confirm-1"))
		}
	}
}

func TestManagerRepeatedDecisionIsRejected(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})
	req := DecisionRequest{UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true}
	if _, err := manager.Decide(context.Background(), req); err != nil {
		t.Fatalf("first Decide: %v", err)
	}
	if _, err := manager.Decide(context.Background(), req); !errors.Is(err, ErrConfirmationAlreadyProcessed) {
		t.Fatalf("second Decide error = %v", err)
	}
	if model.resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", model.resolveCalls)
	}
}

func TestManagerMySQLCASAllowsOnlyOneConcurrentWinner(t *testing.T) {
	model := modelWithPending("confirm-1", fixedNow.Add(time.Minute))
	manager := newTestManager(model, highRiskRegistry(), &fakeLocker{alwaysAcquire: true})
	req := DecisionRequest{UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1", Approved: true}

	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.Decide(context.Background(), req); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful decisions = %d, want 1", successes.Load())
	}
	if model.row("confirm-1").Status != StatusApproved {
		t.Fatalf("row = %#v", model.row("confirm-1"))
	}
}

func TestManagerMarksApprovedConfirmationExecutedOrFailed(t *testing.T) {
	tests := []struct {
		name       string
		complete   func(*Manager, context.Context, CompletionRequest) (*domain.Confirmation, error)
		wantStatus string
	}{
		{name: "executed", complete: (*Manager).MarkExecuted, wantStatus: StatusExecuted},
		{name: "failed", complete: (*Manager).MarkFailed, wantStatus: StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := modelWithStatus("confirm-1", StatusApproved, fixedNow.Add(time.Minute))
			manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})
			result, err := tt.complete(manager, context.Background(), CompletionRequest{
				UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1",
			})
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			row := model.row("confirm-1")
			if result.Status != tt.wantStatus || row.Status != tt.wantStatus || !row.ExecutedAt.Valid || !row.ExecutedAt.Time.Equal(fixedNow) {
				t.Fatalf("result=%#v row=%#v", result, row)
			}
		})
	}
}

func TestManagerCannotCompleteNonApprovedConfirmation(t *testing.T) {
	for _, status := range []string{StatusPending, StatusRejected, StatusExpired, StatusExecuted, StatusFailed} {
		model := modelWithStatus("confirm-1", status, fixedNow.Add(time.Minute))
		manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})
		_, err := manager.MarkExecuted(context.Background(), CompletionRequest{
			UserID: 42, ConversationID: "conv-1", ConfirmationID: "confirm-1",
		})
		if !errors.Is(err, ErrConfirmationAlreadyProcessed) {
			t.Fatalf("status %s error = %v", status, err)
		}
		if model.completeCalls != 0 {
			t.Fatalf("status %s attempted completion", status)
		}
	}
}

func TestManagerCompletionRejectsCrossUserAndConversation(t *testing.T) {
	tests := []CompletionRequest{
		{UserID: 99, ConversationID: "conv-1", ConfirmationID: "confirm-1"},
		{UserID: 42, ConversationID: "conv-other", ConfirmationID: "confirm-1"},
	}
	for _, req := range tests {
		model := modelWithStatus("confirm-1", StatusApproved, fixedNow.Add(time.Minute))
		manager := newTestManager(model, highRiskRegistry(), &fakeLocker{})
		if _, err := manager.MarkExecuted(context.Background(), req); !errors.Is(err, ErrConfirmationForbidden) {
			t.Fatalf("MarkExecuted(%#v) error = %v", req, err)
		}
		if model.completeCalls != 0 || model.row("confirm-1").Status != StatusApproved {
			t.Fatalf("forbidden completion mutated row: %#v", model.row("confirm-1"))
		}
	}
}

func newTestManager(model *fakeModel, registry *fakeRegistry, locker Locker) *Manager {
	return NewManager(model, registry, locker,
		WithClock(func() time.Time { return fixedNow }),
		WithIDGenerator(func() string { return "confirm-fixed" }),
	)
}

type fakeRegistry struct {
	metadata map[string]domain.Metadata
}

func registryWith(values ...domain.Metadata) *fakeRegistry {
	registry := &fakeRegistry{metadata: make(map[string]domain.Metadata)}
	for _, value := range values {
		registry.metadata[value.Name] = value
	}
	return registry
}

func highRiskRegistry() *fakeRegistry {
	return registryWith(domain.Metadata{
		Name: domain.ToolOrderCancel, Risk: domain.RiskHigh,
		RequireConfirmation: true, WriteOperation: true,
	})
}

func (r *fakeRegistry) Metadata(name string) (domain.Metadata, error) {
	metadata, ok := r.metadata[name]
	if !ok {
		return domain.Metadata{}, errors.New("tool not found")
	}
	return metadata, nil
}

type fakeModel struct {
	mu                 sync.Mutex
	rows               map[string]*aiconfirmations.AiConfirmations
	insertCalls        int
	readCalls          int
	resolveCalls       int
	expireCalls        int
	completeCalls      int
	bindCalls          int
	expireWinnerStatus string
	insertResult       sql.Result
	insertErr          error
}

func newFakeModel() *fakeModel {
	return &fakeModel{rows: make(map[string]*aiconfirmations.AiConfirmations)}
}

func modelWithPending(id string, expiresAt time.Time) *fakeModel {
	return modelWithStatus(id, StatusPending, expiresAt)
}

func modelWithStatus(id, status string, expiresAt time.Time) *fakeModel {
	model := newFakeModel()
	model.rows[id] = &aiconfirmations.AiConfirmations{
		Id: id, ConversationId: "conv-1", UserId: 42,
		ToolName: domain.ToolOrderCancel, Arguments: `{"order_id":"order-1"}`,
		Summary: "确认取消订单？", Status: status, ExpiresAt: expiresAt,
	}
	return model
}

func (m *fakeModel) Insert(_ context.Context, row *aiconfirmations.AiConfirmations) (sql.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertCalls++
	m.rows[row.Id] = cloneRow(row)
	return m.insertResult, m.insertErr
}

func (m *fakeModel) FindOneUncached(_ context.Context, id string) (*aiconfirmations.AiConfirmations, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readCalls++
	row, ok := m.rows[id]
	if !ok {
		return nil, aiconfirmations.ErrNotFound
	}
	return cloneRow(row), nil
}

func (m *fakeModel) ResolvePending(_ context.Context, id string, userID uint64, nextStatus string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolveCalls++
	row := m.rows[id]
	if row == nil || row.UserId != userID || row.Status != StatusPending || !row.ExpiresAt.After(now) {
		return false, nil
	}
	row.Status = nextStatus
	return true, nil
}

func (m *fakeModel) ExpirePending(_ context.Context, id string, userID uint64, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireCalls++
	row := m.rows[id]
	if m.expireWinnerStatus != "" {
		row.Status = m.expireWinnerStatus
		return false, nil
	}
	if row == nil || row.UserId != userID || row.Status != StatusPending || row.ExpiresAt.After(now) {
		return false, nil
	}
	row.Status = StatusExpired
	return true, nil
}

func (m *fakeModel) CompleteApproved(_ context.Context, id string, userID uint64, nextStatus string, executedAt time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCalls++
	row := m.rows[id]
	if row == nil || row.UserId != userID || row.Status != StatusApproved {
		return false, nil
	}
	row.Status = nextStatus
	row.ExecutedAt = sql.NullTime{Time: executedAt, Valid: true}
	return true, nil
}

func (m *fakeModel) BindResumeTarget(_ context.Context, id string, userID uint64, runID string, checkpointID string, interruptID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindCalls++
	row := m.rows[id]
	if row == nil || row.UserId != userID || row.Status != StatusPending {
		return false, nil
	}
	row.RunId = runID
	row.CheckpointId = checkpointID
	row.InterruptId = interruptID
	return true, nil
}

func (m *fakeModel) row(id string) *aiconfirmations.AiConfirmations {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneRow(m.rows[id])
}

func cloneRow(row *aiconfirmations.AiConfirmations) *aiconfirmations.AiConfirmations {
	if row == nil {
		return nil
	}
	copied := *row
	return &copied
}

type fakeLocker struct {
	mu            sync.Mutex
	lock          Lock
	acquired      *bool
	err           error
	alwaysAcquire bool
	lastKey       string
	lastTTL       time.Duration
}

func (l *fakeLocker) Acquire(_ context.Context, key string, ttl time.Duration) (Lock, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastKey = key
	l.lastTTL = ttl
	if l.err != nil {
		return nil, false, l.err
	}
	if l.acquired != nil && !*l.acquired {
		return nil, false, nil
	}
	if l.alwaysAcquire {
		return &fakeLock{}, true, nil
	}
	if l.lock == nil {
		l.lock = &fakeLock{}
	}
	return l.lock, true, nil
}

type fakeLock struct {
	mu                 sync.Mutex
	releaseCalls       int
	err                error
	honorContext       bool
	releaseContextErr  error
	releaseHadDeadline bool
}

func (l *fakeLock) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releaseCalls++
	l.releaseContextErr = ctx.Err()
	_, l.releaseHadDeadline = ctx.Deadline()
	if l.honorContext && ctx.Err() != nil {
		return ctx.Err()
	}
	return l.err
}

func boolPtr(value bool) *bool { return &value }

type fakeInsertResult struct{}

func (fakeInsertResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeInsertResult) RowsAffected() (int64, error) { return 1, nil }

package resilience

import (
	"testing"
	"time"
)

func TestCircuitBreaker_OpenAfterConsecutiveFailures(t *testing.T) {
	b := NewCircuitBreaker(BreakerConfig{FailureThreshold: 3})
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatalf("expected closed after 2 failures, got %v", b.State())
	}
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open after 3 failures, got %v", b.State())
	}
}

func TestCircuitBreaker_AllowRejectsWhileOpen(t *testing.T) {
	b := NewCircuitBreaker(BreakerConfig{FailureThreshold: 1})
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %v", b.State())
	}
	// 冷却未到期，直接拒绝
	if b.Allow() {
		t.Fatal("expected reject while open before cooldown")
	}
}

func TestCircuitBreaker_LazyHalfOpen(t *testing.T) {
	var now time.Time
	b := NewCircuitBreaker(BreakerConfig{Cooldown: 30 * time.Second, FailureThreshold: 1})
	b.now = func() time.Time { return now }

	now = time.Unix(1000, 0)
	b.RecordFailure() // 立即熔断
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %v", b.State())
	}

	now = time.Unix(1010, 0) // 10s，冷却未到期
	if b.Allow() {
		t.Fatal("expected reject before cooldown")
	}
	if b.State() != StateOpen {
		t.Fatalf("expected still open before cooldown, got %v", b.State())
	}

	now = time.Unix(1030, 0) // 30s，冷却到期 → 懒半开放行探针
	if !b.Allow() {
		t.Fatal("expected allow after cooldown")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("expected half-open, got %v", b.State())
	}

	// 半开只有一个探针名额，第二个请求被拒
	if b.Allow() {
		t.Fatal("expected reject for second probe (quota exhausted)")
	}
}

func TestCircuitBreaker_ProbeSuccessCloses(t *testing.T) {
	var now time.Time
	b := NewCircuitBreaker(BreakerConfig{Cooldown: 30 * time.Second, FailureThreshold: 1})
	b.now = func() time.Time { return now }

	now = time.Unix(1000, 0)
	b.RecordFailure()
	now = time.Unix(1030, 0)
	if !b.Allow() {
		t.Fatal("expected probe allowed")
	}
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Fatalf("expected closed after probe success, got %v", b.State())
	}
}

func TestCircuitBreaker_ProbeFailureReopens(t *testing.T) {
	var now time.Time
	b := NewCircuitBreaker(BreakerConfig{Cooldown: 30 * time.Second, FailureThreshold: 1})
	b.now = func() time.Time { return now }

	now = time.Unix(1000, 0)
	b.RecordFailure()
	now = time.Unix(1030, 0)
	if !b.Allow() {
		t.Fatal("expected probe allowed")
	}
	b.RecordFailure() // 探针失败 → 重新熔断
	if b.State() != StateOpen {
		t.Fatalf("expected reopen, got %v", b.State())
	}

	// 冷却从 1030 重新起算：1050 仍拒绝，1060 放行
	now = time.Unix(1050, 0)
	if b.Allow() {
		t.Fatal("expected reject before reset cooldown")
	}
	now = time.Unix(1060, 0)
	if !b.Allow() {
		t.Fatal("expected allow after reset cooldown")
	}
}

func TestCircuitBreaker_RecordSuccessResetsConsecutive(t *testing.T) {
	b := NewCircuitBreaker(BreakerConfig{FailureThreshold: 3})
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // 成功清空连续失败
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatalf("expected closed, got %v", b.State())
	}
}

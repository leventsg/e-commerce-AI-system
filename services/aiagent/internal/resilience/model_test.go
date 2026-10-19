package resilience

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeChatModel 是一个可配置的 model.ToolCallingChatModel，用于测试。
type fakeChatModel struct {
	name     string
	genFn    func(ctx context.Context, input []*schema.Message) (*schema.Message, error)
	streamFn func(ctx context.Context, input []*schema.Message) (*schema.StreamReader[*schema.Message], error)
	calls    atomic.Int32
}

func (f *fakeChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.calls.Add(1)
	if f.genFn == nil {
		return &schema.Message{Role: schema.Assistant, Content: "ok"}, nil
	}
	return f.genFn(ctx, input)
}

func (f *fakeChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.calls.Add(1)
	if f.streamFn == nil {
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "ok"}}), nil
	}
	return f.streamFn(ctx, input)
}

func (f *fakeChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func newCandidate(name string, m model.ToolCallingChatModel, priority int) Candidate {
	return Candidate{Model: m, Breaker: NewCircuitBreaker(BreakerConfig{}), Priority: priority, Name: name}
}

func msgContent(c string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: c}
}

// streamWithError 构造一个先产出 chunks、再产出错误 err 的流。
func streamWithError(chunks []*schema.Message, err error) *schema.StreamReader[*schema.Message] {
	sr, sw := schema.Pipe[*schema.Message](len(chunks) + 1)
	go func() {
		defer sw.Close()
		for _, c := range chunks {
			if sw.Send(c, nil) {
				return
			}
		}
		if err != nil {
			sw.Send(nil, err)
		}
	}()
	return sr
}

func drainStream(t *testing.T, sr *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	t.Helper()
	defer sr.Close()
	var out []*schema.Message
	var lastErr error
	for {
		m, err := sr.Recv()
		if err == io.EOF {
			return out, lastErr
		}
		if err != nil {
			lastErr = err
			continue
		}
		if m != nil {
			out = append(out, m)
		}
	}
}

func TestResilientModel_GenerateFallsBack(t *testing.T) {
	primary := &fakeChatModel{name: "primary", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return nil, &fakeStatusError{HTTPStatusCode: 503}
	}}
	backup := &fakeChatModel{name: "backup", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return msgContent("from backup"), nil
	}}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	got, err := m.Generate(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Content != "from backup" {
		t.Fatalf("expected backup content, got %q", got.Content)
	}
	if primary.calls.Load() != 1 || backup.calls.Load() != 1 {
		t.Fatalf("expected 1 call each, got primary=%d backup=%d", primary.calls.Load(), backup.calls.Load())
	}
}

func TestResilientModel_GenerateAllFail(t *testing.T) {
	primary := &fakeChatModel{name: "primary", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("boom")
	}}
	backup := &fakeChatModel{name: "backup", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("also down")
	}}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	_, err := m.Generate(context.Background(), []*schema.Message{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAllModelsUnavailable) {
		t.Fatalf("expected ErrAllModelsUnavailable, got %v", err)
	}
}

func TestResilientModel_GenerateSkipsOpenBreaker(t *testing.T) {
	primary := &fakeChatModel{name: "primary"}
	backup := &fakeChatModel{name: "backup", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return msgContent("from backup"), nil
	}}
	primaryCand := newCandidate("primary", primary, 0)
	// 手动把主候选熔断
	primaryCand.Breaker.RecordFailure()
	primaryCand.Breaker.RecordFailure()
	primaryCand.Breaker.RecordFailure()
	if primaryCand.Breaker.State() != StateOpen {
		t.Fatalf("expected open breaker, got %v", primaryCand.Breaker.State())
	}

	m := NewResilientModel([]Candidate{primaryCand, newCandidate("backup", backup, 1)}, RetryConfig{})
	got, err := m.Generate(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Content != "from backup" {
		t.Fatalf("expected backup content, got %q", got.Content)
	}
	if primary.calls.Load() != 0 {
		t.Fatalf("primary should be skipped, got %d calls", primary.calls.Load())
	}
}

func TestResilientModel_GenerateRetriesRetryable(t *testing.T) {
	primary := &fakeChatModel{name: "primary", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return nil, &fakeStatusError{HTTPStatusCode: 503}
	}}
	m := NewResilientModel([]Candidate{newCandidate("primary", primary, 0)}, RetryConfig{
		MaxRetries: 2,
		BaseDelay:  time.Millisecond,
	})

	_, err := m.Generate(context.Background(), []*schema.Message{})
	if err == nil {
		t.Fatal("expected error after retries")
	}
	// 1 次初始 + 2 次重试 = 3 次调用
	if primary.calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", primary.calls.Load())
	}
}

func TestResilientModel_GenerateNoRetryNonRetryable(t *testing.T) {
	primary := &fakeChatModel{name: "primary", genFn: func(ctx context.Context, _ []*schema.Message) (*schema.Message, error) {
		return nil, &fakeStatusError{HTTPStatusCode: 401}
	}}
	m := NewResilientModel([]Candidate{newCandidate("primary", primary, 0)}, RetryConfig{
		MaxRetries: 5,
		BaseDelay:  time.Millisecond,
	})

	_, err := m.Generate(context.Background(), []*schema.Message{})
	if err == nil {
		t.Fatal("expected error")
	}
	if primary.calls.Load() != 1 {
		t.Fatalf("non-retryable should not retry, expected 1 call got %d", primary.calls.Load())
	}
}

func TestResilientModel_StreamSwitchesOnFirstByteFailure(t *testing.T) {
	primary := &fakeChatModel{name: "primary", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		// Stream() 成功建立，但首字节前就报错
		return streamWithError(nil, &fakeStatusError{HTTPStatusCode: 503}), nil
	}}
	backup := &fakeChatModel{name: "backup", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		return schema.StreamReaderFromArray([]*schema.Message{msgContent("hello"), msgContent(" world")}), nil
	}}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	sr, err := m.Stream(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got, lastErr := drainStream(t, sr)
	if lastErr != nil {
		t.Fatalf("expected clean stream, got error %v", lastErr)
	}
	if len(got) != 2 || got[0].Content != "hello" || got[1].Content != " world" {
		t.Fatalf("expected backup chunks, got %+v", got)
	}
	if primary.calls.Load() != 1 || backup.calls.Load() != 1 {
		t.Fatalf("expected 1 call each, got primary=%d backup=%d", primary.calls.Load(), backup.calls.Load())
	}
}

func TestResilientModel_StreamNoSwitchAfterCommitted(t *testing.T) {
	var backupCalled atomic.Bool
	primary := &fakeChatModel{name: "primary", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		// 先出一个内容 chunk，再报错
		return streamWithError([]*schema.Message{msgContent("partial")}, &fakeStatusError{HTTPStatusCode: 503}), nil
	}}
	backup := &fakeChatModel{name: "backup", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		backupCalled.Store(true)
		return schema.StreamReaderFromArray([]*schema.Message{msgContent("backup")}), nil
	}}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	sr, err := m.Stream(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got, lastErr := drainStream(t, sr)
	if len(got) != 1 || got[0].Content != "partial" {
		t.Fatalf("expected only primary partial chunk, got %+v", got)
	}
	if lastErr == nil {
		t.Fatal("expected stream error after committed chunk")
	}
	if backupCalled.Load() {
		t.Fatal("backup should not be called after committed chunk")
	}
}

func TestResilientModel_StreamAllEstablishFail(t *testing.T) {
	primary := &fakeChatModel{name: "primary", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		return nil, &fakeStatusError{HTTPStatusCode: 503}
	}}
	backup := &fakeChatModel{name: "backup", streamFn: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		return nil, &fakeStatusError{HTTPStatusCode: 503}
	}}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	_, err := m.Stream(context.Background(), []*schema.Message{})
	if err == nil {
		t.Fatal("expected error when all candidates fail to establish")
	}
	if !errors.Is(err, ErrAllModelsUnavailable) {
		t.Fatalf("expected ErrAllModelsUnavailable, got %v", err)
	}
}

func TestResilientModel_WithTools(t *testing.T) {
	primary := &fakeChatModel{name: "primary"}
	backup := &fakeChatModel{name: "backup"}
	m := NewResilientModel([]Candidate{
		newCandidate("primary", primary, 0),
		newCandidate("backup", backup, 1),
	}, RetryConfig{})

	bound, err := m.WithTools(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bound == nil {
		t.Fatal("expected non-nil bound model")
	}
	if _, ok := bound.(model.ToolCallingChatModel); !ok {
		t.Fatal("expected ToolCallingChatModel")
	}
}

func TestResilientModel_CandidatesSortedByPriority(t *testing.T) {
	a := newCandidate("low", &fakeChatModel{}, 5)
	b := newCandidate("high", &fakeChatModel{}, 0)
	c := newCandidate("mid", &fakeChatModel{}, 2)
	m := NewResilientModel([]Candidate{a, b, c}, RetryConfig{})
	got := m.Candidates()
	if got[0].Name != "high" || got[1].Name != "mid" || got[2].Name != "low" {
		t.Fatalf("expected [high mid low], got %v", []string{got[0].Name, got[1].Name, got[2].Name})
	}
}

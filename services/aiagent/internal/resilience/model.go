package resilience

import (
	"context"
	"errors"
	"io"
	"sort"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ErrAllModelsUnavailable 表示候选池中所有模型都不可用（全部熔断或全部失败）。
var ErrAllModelsUnavailable = errors.New("all candidate models unavailable")

// Candidate 候选池中的一个模型，绑定独立的熔断器与优先级。
type Candidate struct {
	// Model 底层工具调用模型（已绑定工具）。
	Model model.ToolCallingChatModel
	// Breaker 该候选独立的熔断器。
	Breaker *CircuitBreaker
	// Priority 优先级，数值越小越靠前（主模型为 0）。
	Priority int
	// Name 模型标识（provider/model），用于日志与观测。
	Name string
}

// ResilientModel 实现 model.ToolCallingChatModel，把候选池、熔断、重试、降级
// 封装成一个对上层 agent 完全透明的模型。
type ResilientModel struct {
	candidates []Candidate // 构造时按 Priority 升序
	retry      RetryConfig
}

// NewResilientModel 创建候选池模型，候选按 Priority 升序排序。
func NewResilientModel(candidates []Candidate, retry RetryConfig) *ResilientModel {
	sorted := make([]Candidate, len(candidates))
	copy(sorted, candidates)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return &ResilientModel{candidates: sorted, retry: retry}
}

// Candidates 返回按优先级排序后的候选快照（供观测/测试）。
func (m *ResilientModel) Candidates() []Candidate {
	out := make([]Candidate, len(m.candidates))
	copy(out, m.candidates)
	return out
}

// Generate 非流式调用：沿候选池按优先级降级，候选内做可重试错误重试。
// 非流式无副作用，且不存在「已提交」问题，可安全换候选。
func (m *ResilientModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	var lastErr error
	for _, c := range m.candidates {
		if !c.Breaker.Allow() {
			continue // 熔断/半开无配额，降级到下一候选
		}
		msg, err := m.callWithRetry(ctx, c, func(ctx context.Context) (*schema.Message, error) {
			return c.Model.Generate(ctx, input, opts...)
		})
		if err == nil {
			c.Breaker.RecordSuccess()
			return msg, nil
		}
		c.Breaker.RecordFailure()
		lastErr = err
	}
	return nil, wrapAllUnavailable(lastErr)
}

// Stream 流式调用：返回一个「可切换候选」的 reader。初始建立流时选第一个可用
// 候选；首个有内容字节前的断流会自动切到下一候选（完整版语义，见 switchingStreamReader）。
func (m *ResilientModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	r := &switchingStreamReader{
		pool:   m,
		ctx:    ctx,
		input:  input,
		opts:   opts,
		idx:    -1,
		writer: sw,
	}
	// 同步建立第一个可用候选的流：全部候选建立失败时，把错误作为 Stream 的返回值
	// 立即抛给上层（与 Eino 模型调用失败的语义一致），而非异步经 reader 透传。
	if err := r.advance(); err != nil {
		sw.Close()
		return nil, wrapAllUnavailable(err)
	}
	go r.pump()
	return sr, nil
}

// WithTools 将工具绑定到所有候选，返回新的 ResilientModel（共享熔断器）。
func (m *ResilientModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound := make([]Candidate, len(m.candidates))
	for i, c := range m.candidates {
		modelWithTools, err := c.Model.WithTools(tools)
		if err != nil {
			return nil, err
		}
		bound[i] = Candidate{Model: modelWithTools, Breaker: c.Breaker, Priority: c.Priority, Name: c.Name}
	}
	return &ResilientModel{candidates: bound, retry: m.retry}, nil
}

// callWithRetry 对单个候选执行调用，仅对「可重试」错误做退避重试。
func (m *ResilientModel) callWithRetry(ctx context.Context, c Candidate, call func(context.Context) (*schema.Message, error)) (*schema.Message, error) {
	retryCfg := m.retry.Normalized()
	for attempt := 0; ; attempt++ {
		msg, err := call(ctx)
		if err == nil {
			return msg, nil
		}
		class := Classify(err)
		if !class.Retryable() {
			return nil, err // 不可重试，直接返回
		}
		if attempt >= retryCfg.MaxRetries {
			return nil, err // 重试耗尽
		}
		if waitErr := waitRetryDelay(ctx, retryCfg.backoff(class, attempt)); waitErr != nil {
			return nil, waitErr
		}
	}
}

// switchingStreamReader 是一个流式 reader 的候选切换器。它消费底层候选的流，
// 并在「首个有内容字节之前」发生错误时，自动切到下一候选从头重流同一份 messages。
type switchingStreamReader struct {
	pool   *ResilientModel
	ctx    context.Context
	input  []*schema.Message
	opts   []model.Option
	idx    int
	cur    *schema.StreamReader[*schema.Message]
	writer *schema.StreamWriter[*schema.Message]
}

// advance 走到下一个可用候选并建立流；全部不可用返回 ErrAllModelsUnavailable。
func (r *switchingStreamReader) advance() error {
	for r.idx+1 < len(r.pool.candidates) {
		r.idx++
		c := r.pool.candidates[r.idx]
		if !c.Breaker.Allow() {
			continue // 熔断/半开无配额，跳过
		}
		reader, err := c.Model.Stream(r.ctx, r.input, r.opts...)
		if err != nil {
			c.Breaker.RecordFailure()
			continue
		}
		c.Breaker.RecordSuccess()
		r.cur = reader
		return nil
	}
	return ErrAllModelsUnavailable
}

func (r *switchingStreamReader) closeCur() {
	if r.cur != nil {
		r.cur.Close()
		r.cur = nil
	}
}

// pump 驱动候选流：读 chunk 写入 pipe；首字节前出错则换候选，已提交则透传错误。
func (r *switchingStreamReader) pump() {
	defer func() {
		r.closeCur()
		r.writer.Close()
	}()
	committed := false
	for {
		if r.cur == nil {
			r.writer.Send(nil, ErrAllModelsUnavailable)
			return
		}
		msg, err := r.cur.Recv()
		if err == io.EOF {
			return // 正常结束
		}
		if err != nil {
			if !committed {
				// 首个有内容字节前失败 → 换候选从头重流（同一份 messages，无副作用）
				r.pool.candidates[r.idx].Breaker.RecordFailure()
				r.closeCur()
				if advErr := r.advance(); advErr != nil {
					r.writer.Send(nil, advErr)
					return
				}
				continue
			}
			// 已提交，透传错误给上层，走「优雅收尾」逻辑
			r.writer.Send(nil, err)
			return
		}
		if msg == nil {
			continue
		}
		if hasCommittedContent(msg) {
			committed = true
		}
		if closed := r.writer.Send(msg, nil); closed {
			return
		}
	}
}

// hasCommittedContent 判断 chunk 是否为「有内容」chunk（文字/工具调用/思考）。
// 空 chunk 不算提交点，因此「一堆空 chunk 后断流」仍可换候选。
func hasCommittedContent(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	if msg.Content != "" || len(msg.ToolCalls) > 0 || msg.ReasoningContent != "" {
		return true
	}
	if v, ok := msg.Extra["reasoning-content"].(string); ok && v != "" {
		return true
	}
	return false
}

func wrapAllUnavailable(lastErr error) error {
	if lastErr == nil || errors.Is(lastErr, ErrAllModelsUnavailable) {
		return ErrAllModelsUnavailable
	}
	return errors.Join(ErrAllModelsUnavailable, lastErr)
}

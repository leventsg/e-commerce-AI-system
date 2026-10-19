// Package resilience 提供模型侧的稳定性与降级能力：每模型熔断器、
// 候选池优先级路由、可重试/不可重试错误分类、以及流式首字节前的候选切换。
//
// 设计原则（见 docs/model-resilience-design.md）：
//   - 重试/降级只发生在模型层，不发生在 agent 层，避免重跑 ToolsNode 造成重复副作用。
//   - 熔断采用「懒半开探针」：不设后台定时器，请求到来时才判断冷却是否到期并放行单个探针。
package resilience

import (
	"sync/atomic"
	"time"
)

// BreakerState 熔断器三态。
type BreakerState int32

const (
	// StateClosed 闭合：正常放行，累计连续失败计数。
	StateClosed BreakerState = iota
	// StateOpen 熔断：直接拒绝，不发起模型调用。
	StateOpen
	// StateHalfOpen 半开：冷却到期后放行有限个探针验证恢复。
	StateHalfOpen
)

// BreakerConfig 熔断器配置。字段为 0 时在 NewCircuitBreaker 内填默认值。
type BreakerConfig struct {
	// Cooldown 熔断后进入半开前的冷却窗口。默认 30s。
	Cooldown time.Duration
	// FailureThreshold 连续失败多少次才触发熔断。默认 3。
	FailureThreshold uint32
	// HalfOpenMaxProbe 半开状态下允许通过的探针数。默认 1。
	HalfOpenMaxProbe uint32
}

const (
	defaultCooldown         = 30 * time.Second
	defaultFailureThreshold = 3
	defaultHalfOpenMaxProbe = 1
)

// CircuitBreaker 是一个线程安全的熔断器实现。
//
// 懒半开：冷却到期不主动探测，而是等下一个请求到来时，由 Allow 检测到期并
// 用该请求作为探针放行。这样空闲时段不会空转探测一个已经恢复的模型，也省调用成本。
type CircuitBreaker struct {
	state         atomic.Int32
	consecutive   atomic.Uint32
	openedAt      atomic.Int64 // unix nano，熔断发生时刻
	halfOpenSlots atomic.Int32 // 半开探针配额计数
	cfg           BreakerConfig
	now           func() time.Time // 可注入时钟，便于测试
}

// NewCircuitBreaker 创建熔断器，并填充默认配置。
func NewCircuitBreaker(cfg BreakerConfig) *CircuitBreaker {
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = defaultCooldown
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}
	if cfg.HalfOpenMaxProbe == 0 {
		cfg.HalfOpenMaxProbe = defaultHalfOpenMaxProbe
	}
	return &CircuitBreaker{cfg: cfg, now: time.Now}
}

// State 返回当前状态。
func (b *CircuitBreaker) State() BreakerState {
	return BreakerState(b.state.Load())
}

// Allow 返回是否放行本次调用。熔断或半开无配额时返回 false。
//
// 懒半开逻辑：状态为 Open 时，若冷却已到期，则 CAS 切到 HalfOpen 并用本请求
// 作为探针放行；冷却未到期则继续拒绝，不发起任何真实调用。
func (b *CircuitBreaker) Allow() bool {
	for {
		switch BreakerState(b.state.Load()) {
		case StateClosed:
			return true
		case StateHalfOpen:
			if b.halfOpenSlots.Add(1) <= int32(b.cfg.HalfOpenMaxProbe) {
				return true
			}
			b.halfOpenSlots.Add(-1) // 探针名额已满，回滚
			return false
		case StateOpen:
			if b.now().UnixNano() < b.openedAt.Load()+int64(b.cfg.Cooldown) {
				return false // 冷却未到期，继续熔断
			}
			if b.state.CompareAndSwap(int32(StateOpen), int32(StateHalfOpen)) {
				b.halfOpenSlots.Store(0)
				b.halfOpenSlots.Add(1)
				return true
			}
			// CAS 失败说明并发已有其他请求切到半开，继续循环
		}
	}
}

// RecordSuccess 记录一次成功调用：清空连续失败计数；若处于半开态，探针成功则关闭熔断。
func (b *CircuitBreaker) RecordSuccess() {
	b.consecutive.Store(0)
	if BreakerState(b.state.Load()) == StateHalfOpen {
		b.state.CompareAndSwap(int32(StateHalfOpen), int32(StateClosed))
		b.halfOpenSlots.Store(0)
	}
}

// RecordFailure 记录一次失败调用：半开态探针失败立即重新熔断；闭态累加连续失败，达阈值熔断。
func (b *CircuitBreaker) RecordFailure() {
	if BreakerState(b.state.Load()) == StateHalfOpen {
		b.trip()
		return
	}
	if b.consecutive.Add(1) >= b.cfg.FailureThreshold {
		b.trip()
	}
}

func (b *CircuitBreaker) trip() {
	b.state.Store(int32(StateOpen))
	b.openedAt.Store(b.now().UnixNano())
	b.halfOpenSlots.Store(0)
}

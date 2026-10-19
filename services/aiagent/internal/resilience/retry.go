package resilience

import (
	"context"
	"math/rand"
	"time"
)

// RetryConfig 单候选内重试配置。字段为 0 时由 Normalized 填默认值。
type RetryConfig struct {
	// MaxRetries 最大重试次数（不含首次）。默认 3。
	MaxRetries int
	// BaseDelay 退避基数。默认 200ms。
	BaseDelay time.Duration
	// RateLimitBaseDelay 429 限流时的退避基数，通常更大。默认 1s。
	RateLimitBaseDelay time.Duration
}

const (
	defaultMaxRetries         = 3
	defaultBaseDelay          = 200 * time.Millisecond
	defaultRateLimitBaseDelay = time.Second
)

// Normalized 返回填充默认值后的配置副本。
func (c RetryConfig) Normalized() RetryConfig {
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = defaultBaseDelay
	}
	if c.RateLimitBaseDelay <= 0 {
		c.RateLimitBaseDelay = defaultRateLimitBaseDelay
	}
	return c
}

// backoff 计算第 attempt 次重试（attempt 从 0 起）的退避延迟，使用指数退避 + 全抖动。
// 全抖动（rand[0, cap]）避免故障恢复瞬间的惊群效应。
func (c RetryConfig) backoff(class ErrorClass, attempt int) time.Duration {
	base := c.BaseDelay
	if class == ClassRateLimited {
		base = c.RateLimitBaseDelay
	}
	// cap = base * 2^attempt，封顶避免无限增长
	capDelay := base
	for i := 0; i < attempt && capDelay < 30*time.Second; i++ {
		capDelay *= 2
	}
	if capDelay <= 0 {
		return 0
	}
	// full jitter: [0, capDelay]
	return time.Duration(rand.Int63n(int64(capDelay) + 1))
}

// waitRetryDelay 等待指定延迟，若 ctx 取消则立即返回 ctx.Err。
func waitRetryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryable 报告该错误类是否应进入重试循环（不含重试耗尽判断）。
func retryable(class ErrorClass) bool {
	return class.Retryable()
}

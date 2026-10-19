package eino

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/resilience"
)

// NewResilientModelFactory 返回一个候选池感知的 ModelFactory。
//
// 当 pool 无候选时，退化为 base 的单模型行为（NewChatModel 使用传入的 cfg）；
// 否则 NewChatModel 按 pool 逐候选构建底层模型 + 独立熔断器，封装为
// resilience.ResilientModel（候选按优先级降级 + 熔断 + 重试）。
//
// 熔断器按「模型身份」跨 NewChatModel 调用共享：supervisor 与各子 agent 各自
// 调用 NewChatModel，但同一模型（provider/baseURL/model/apiKey 相同）复用同一个
// CircuitBreaker，从而共享健康状态。
func NewResilientModelFactory(base ModelFactory, pool config.ModelPoolConfig) ModelFactory {
	return &resilientModelFactory{base: base, pool: pool}
}

type resilientModelFactory struct {
	base     ModelFactory
	pool     config.ModelPoolConfig
	breakers sync.Map // modelIdentity -> *resilience.CircuitBreaker
}

func (f *resilientModelFactory) NewChatModel(ctx context.Context, cfg config.EinoConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error) {
	if len(f.pool.Candidates) == 0 {
		return f.base.NewChatModel(ctx, cfg, tools...)
	}

	candidates := make([]resilience.Candidate, 0, len(f.pool.Candidates))
	for _, mc := range f.pool.Candidates {
		chatModel, err := f.base.NewChatModel(ctx, mc.EinoConfig, tools...)
		if err != nil {
			// 候选构建失败（空模型名/不支持的 provider）是配置错误，应启动即失败而非静默降级。
			return nil, fmt.Errorf("build candidate model %q: %w", mc.Model, err)
		}
		tcModel, ok := chatModel.(model.ToolCallingChatModel)
		if !ok {
			return nil, fmt.Errorf("%w: candidate model %q does not support tool calling", ErrModelUnavailable, mc.Model)
		}
		candidates = append(candidates, resilience.Candidate{
			Model:    tcModel,
			Breaker:  f.breakerFor(mc),
			Priority: mc.Priority,
			Name:     modelName(mc.EinoConfig),
		})
	}

	retry := resilience.RetryConfig{
		MaxRetries: f.pool.DefaultMaxRetries,
		BaseDelay:  time.Duration(f.pool.RetryBaseDelayMs) * time.Millisecond,
	}
	return resilience.NewResilientModel(candidates, retry), nil
}

func (f *resilientModelFactory) NewStructuredChatModel(ctx context.Context, cfg config.EinoConfig, structured StructuredOutputConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error) {
	// 结构化输出模型（画像/记忆抽取）暂不接入候选池，保持单模型行为。
	return f.base.NewStructuredChatModel(ctx, cfg, structured, tools...)
}

// breakerFor 按模型身份取或建熔断器，支持候选级冷却/阈值覆盖。
func (f *resilientModelFactory) breakerFor(mc config.ModelCandidate) *resilience.CircuitBreaker {
	key := modelIdentity(mc.EinoConfig)
	if v, ok := f.breakers.Load(key); ok {
		return v.(*resilience.CircuitBreaker)
	}

	cfg := resilience.BreakerConfig{
		Cooldown:         time.Duration(f.pool.DefaultCooldown) * time.Second,
		FailureThreshold: uint32(f.pool.DefaultThreshold),
	}
	if mc.CooldownSeconds != nil {
		cfg.Cooldown = time.Duration(*mc.CooldownSeconds) * time.Second
	}
	if mc.FailureThreshold != nil {
		cfg.FailureThreshold = uint32(*mc.FailureThreshold)
	}

	b := resilience.NewCircuitBreaker(cfg)
	actual, _ := f.breakers.LoadOrStore(key, b)
	return actual.(*resilience.CircuitBreaker)
}

// modelIdentity 计算模型身份，用于跨 agent 共享熔断状态。
func modelIdentity(cfg config.EinoConfig) string {
	return cfg.Provider + "\x00" + cfg.BaseURL + "\x00" + cfg.Model + "\x00" + cfg.APIKey
}

func modelName(cfg config.EinoConfig) string {
	provider := cfg.Provider
	if provider == "" {
		provider = "openai-compatible"
	}
	modelName := cfg.Model
	if modelName == "" {
		modelName = "<unknown>"
	}
	return provider + "/" + modelName
}

package config

import (
	"github.com/leventsg/e-commerce-AI-system/common/config"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
)

type EinoConfig struct {
	Provider    string
	APIKey      string
	BaseURL     string
	Model       string
	Timeout     int64
	MaxTokens   int
	Temperature float64
}

// ModelCandidate 候选模型池中的一个模型配置（嵌入 EinoConfig 复用单模型字段）。
type ModelCandidate struct {
	EinoConfig
	// Priority 优先级，数值越小越靠前（主模型为 0）。
	Priority int `json:",optional"`
	// CooldownSeconds 该候选熔断冷却秒数，覆盖 ModelPoolConfig.DefaultCooldown。
	CooldownSeconds *int `json:",optional"`
	// FailureThreshold 该候选连续失败熔断阈值，覆盖 ModelPoolConfig.DefaultThreshold。
	FailureThreshold *int `json:",optional"`
}

// ModelPoolConfig 候选模型池配置。Candidates 为空时回退单模型（Config.Eino）。
type ModelPoolConfig struct {
	Candidates []ModelCandidate `json:",optional"`
	// DefaultCooldown 熔断冷却秒数，默认 30。
	DefaultCooldown int `json:",default=30"`
	// DefaultThreshold 连续失败熔断阈值，默认 3。
	DefaultThreshold int `json:",default=3"`
	// DefaultMaxRetries 单候选内最大重试次数，默认 3。
	DefaultMaxRetries int `json:",default=3"`
	// RetryBaseDelayMs 重试退避基数（毫秒），默认 200。
	RetryBaseDelayMs int `json:",default=200"`
}

type LangfuseConfig struct {
	Enabled      bool    `json:",default=false"`
	Host         string  `json:",optional"`
	PublicKey    string  `json:",optional"`
	SecretKey    string  `json:",optional"`
	UserHashSalt string  `json:",optional"`
	Environment  string  `json:",default=development"`
	Release      string  `json:",optional"`
	SampleRate   float64 `json:",default=1"`
	ContentMode  string  `json:",default=summary"`
}

type ToolTimeoutConfig struct {
	QuerySeconds int64
	WriteSeconds int64
}

// SkillConfig 配置化加载 Skill：Dir 指向外置 SKILL.md 目录（覆盖同名内嵌、
// 追加新 Skill），Bindings 覆盖进程级 agent→skill 绑定。二者为空时回退内嵌默认。
type SkillConfig struct {
	Dir      string              `json:",optional"`
	Bindings map[string][]string `json:",optional"`
}

type ConfirmationConfig struct {
	ExpireSeconds     int64
	LockExpireSeconds int64
}

type OrderAPIConfig struct {
	BaseURL          string
	Timeout          int64
	InternalClientIP string
}

type RAGConfig struct {
	Enabled                   bool
	BaseURL                   string
	APIKey                    string
	RetrievePath              string
	EmbeddingPath             string
	Timeout                   int64
	TopK                      int
	ConfidenceThreshold       float64
	VectorSimilarityThreshold float64
	CacheTTLSeconds           int64
	EmbeddingDimension        int
	PreviewBaseURL            string
	SourcePreviewChars        int
	RecentMessages            int
	EmbeddingModel            string
}

// TokenBudgetConfig 上下文 token 预算配置。将上下文压缩的决策依据从"消息条数"
// 切换为"token 预算"：最近消息窗口按预算滑动、摘要触发按累计 token 阈值、各非消息层按 cap 截断。
// 字段全部为 0 时由 contextmanager.TokenBudget.Normalized 填充默认值。
type TokenBudgetConfig struct {
	TotalTokens               int
	RecentWindowTokens        int
	SummaryMaxTokens          int
	LatestToolResultMaxTokens int
	ToolHistoryMaxTokens      int
	TaskStateMaxTokens        int
	UserProfileMaxTokens      int
	MinRecentMessages         int
	SummaryTriggerTokens      int
	SafetyFactor              float64
}

type Config struct {
	zrpc.RpcServerConf
	Consul       consul.Conf
	MysqlConfig  config.MysqlConfig
	RedisConf    redis.RedisConf
	Cache        cache.CacheConf
	Eino         EinoConfig
	ModelPool    ModelPoolConfig `json:",optional"`
	Langfuse     LangfuseConfig  `json:",optional"`
	Skill        SkillConfig     `json:",optional"`
	SummaryModel EinoConfig
	ProfileModel EinoConfig
	KafkaMQ      config.KafkaConfig
	ToolTimeout  ToolTimeoutConfig
	Confirmation ConfirmationConfig
	OrderAPI     OrderAPIConfig
	RAG          RAGConfig
	TokenBudget  TokenBudgetConfig
	ProductRpc   zrpc.RpcClientConf
	InventoryRpc zrpc.RpcClientConf
	OrderRpc     zrpc.RpcClientConf
	CheckoutRpc  zrpc.RpcClientConf
	CartRpc      zrpc.RpcClientConf
	CouponRpc    zrpc.RpcClientConf
	UserRpc      zrpc.RpcClientConf
	AuditRpc     zrpc.RpcClientConf
}

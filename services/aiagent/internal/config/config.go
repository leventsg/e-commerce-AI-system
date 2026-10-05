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

type Config struct {
	zrpc.RpcServerConf
	Consul       consul.Conf
	MysqlConfig  config.MysqlConfig
	RedisConf    redis.RedisConf
	Cache        cache.CacheConf
	Eino         EinoConfig
	Langfuse     LangfuseConfig `json:",optional"`
	Skill        SkillConfig    `json:",optional"`
	SummaryModel EinoConfig
	ProfileModel EinoConfig
	KafkaMQ      config.KafkaConfig
	ToolTimeout  ToolTimeoutConfig
	Confirmation ConfirmationConfig
	OrderAPI     OrderAPIConfig
	RAG          RAGConfig
	ProductRpc   zrpc.RpcClientConf
	InventoryRpc zrpc.RpcClientConf
	OrderRpc     zrpc.RpcClientConf
	CheckoutRpc  zrpc.RpcClientConf
	CartRpc      zrpc.RpcClientConf
	CouponRpc    zrpc.RpcClientConf
	UserRpc      zrpc.RpcClientConf
	AuditRpc     zrpc.RpcClientConf
}

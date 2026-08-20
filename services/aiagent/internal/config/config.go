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

type ToolTimeoutConfig struct {
	QuerySeconds int64
	WriteSeconds int64
}

type ConfirmationConfig struct {
	ExpireSeconds     int64
	LockExpireSeconds int64
}

type OrderAPIConfig struct {
	BaseURL string
	Timeout int64
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
	AuditRpc     zrpc.RpcClientConf
}

package config

import (
	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
)

type Config struct {
	zrpc.RpcServerConf
	Consul         consul.Conf
	MysqlConfig    config.MysqlConfig
	RedisConf      redis.RedisConf
	Alipay         AlipayConfig
	OrderRpc       zrpc.RpcClientConf
	RabbitMQConfig config.RabbitMQConfig
	KafkaMQ        config.KafkaConfig
	Outbox         config.OutboxConfig `json:",optional"`
}

type AlipayConfig struct {
	AppId           string
	PrivateKey      string
	AlipayPublicKey string
	NotifyURL       string
	NotifyPath      string
	NotifyPort      int
	ReturnURL       string
}

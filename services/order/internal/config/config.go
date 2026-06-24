package config

import (
	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
)

type Config struct {
	zrpc.RpcServerConf
	MysqlConfig    config.MysqlConfig
	Consul         consul.Conf
	CheckoutRpc    zrpc.RpcClientConf
	CouponRpc      zrpc.RpcClientConf
	UserRpc        zrpc.RpcClientConf
	InventoryRpc   zrpc.RpcClientConf
	RedisConf      redis.RedisConf
	RabbitMQConfig config.RabbitMQConfig
	KafkaMQ        config.KafkaConfig
	Outbox         config.OutboxConfig `json:",optional"`
}

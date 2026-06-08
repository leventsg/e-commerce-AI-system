package config

import (
	"github.com/leventsg/e-commerce-AI-system/common/config"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
)

type Config struct {
	zrpc.RpcServerConf
	Consul      consul.Conf
	MysqlConfig config.MysqlConfig
	KafkaMQ     config.KafkaConfig
	RedisConf   redis.RedisConf
	ProductRpc  zrpc.RpcClientConf
}

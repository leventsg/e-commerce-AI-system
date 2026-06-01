package config

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
	"github.com/leventsg/e-commerce-AI-system/common/config"
)

type Config struct {
	// gRPC 配置
	zrpc.RpcServerConf
	MysqlConfig   MysqlConfig
	RedisConf     redis.RedisConf
	ElasticSearch config.ElasticSearchConfig
	QiNiu         QiNiu
	Consul        consul.Conf
	InventoryRpc  zrpc.RpcClientConf
	GorseConfig   config.GorseConfig
}
type MysqlConfig struct {
	DataSource  string
	Conntimeout int
}

type QiNiu struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Domain    string
}

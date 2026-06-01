package config

import (
	"github.com/leventsg/e-commerce-AI-system/common/config"

	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
)

type Config struct {
	zrpc.RpcServerConf
	Consul        consul.Conf
	RabbitMQ      config.RabbitMQConfig
	MysqlConfig   config.MysqlConfig
	ElasticSearch config.ElasticSearchConfig
	KafkaMQ       config.KafkaConfig
}

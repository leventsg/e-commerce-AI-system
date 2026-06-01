package config

import (
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
	"github.com/leventsg/e-commerce-AI-system/common/config"
)

type Config struct {
	zrpc.RpcServerConf
	MysqlConfig    config.MysqlConfig
	Consul         consul.Conf
	CheckoutRpc    zrpc.RpcClientConf
	CouponRpc      zrpc.RpcClientConf
	UserRpc        zrpc.RpcClientConf
	InventoryRpc   zrpc.RpcClientConf
	RabbitMQConfig config.RabbitMQConfig
}

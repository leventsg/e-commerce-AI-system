package main

import (
	"flag"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"

	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/consumer"
	_ "github.com/leventsg/e-commerce-AI-system/services/inventory/internal/consumer/cancel_order"
	_ "github.com/leventsg/e-commerce-AI-system/services/inventory/internal/consumer/checkout_timeout"
	_ "github.com/leventsg/e-commerce-AI-system/services/inventory/internal/consumer/timeout_order"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/server"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/inventory.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config

	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		inventory.RegisterInventoryServer(grpcServer, server.NewInventoryServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	if err := consul.RegisterService(c.ListenOn, c.Consul); err != nil {
		logx.Errorw("register service error", logx.Field("err", err))
		panic(err)
	}
	defer s.Stop()

	// 注册MQ消费者
	if err := consumer.Init(c); err != nil {
		logx.Errorw("init consumer error", logx.Field("err", err))
		panic(err)
	}

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}

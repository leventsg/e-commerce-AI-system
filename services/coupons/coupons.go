package main

import (
	"flag"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"

	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer"
	_ "github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer/cancel_order"
	_ "github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer/payment_success"
	_ "github.com/leventsg/e-commerce-AI-system/services/coupons/internal/consumer/timeout_order"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/server"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/coupons.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		coupons.RegisterCouponsServer(grpcServer, server.NewCouponsServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	// 注册服务
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

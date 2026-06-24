package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/consumer"
	_ "github.com/leventsg/e-commerce-AI-system/services/order/internal/consumer/timeout_order"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/delaytask"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/server"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/zero-contrib/zrpc/registry/consul"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/order.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		order.RegisterOrderServiceServer(grpcServer, server.NewOrderServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	if err := consul.RegisterService(c.ListenOn, c.Consul); err != nil {
		logx.Errorw("register service error", logx.Field("err", err))
		panic(err)
	}
	defer s.Stop()

	// 初始化取消订单消息投递器，定时扫描并投递消息到mq (mysql -> mq)
	outboxCtx, cancelOutbox := context.WithCancel(context.Background())
	defer cancelOutbox()
	if ctx.Outbox != nil {
		go ctx.Outbox.Run(outboxCtx)
	}

	// 初始化订单超时扫描器，定时扫描超时订单并写入Outbox消息表（redis -> mysql）
	timeoutScannerCtx, cancelTimeoutScanner := context.WithCancel(context.Background())
	defer cancelTimeoutScanner()
	timeoutScanner := delaytask.NewOrderTimeoutScanner(c, ctx.RedisClient, ctx.OrderModel, ctx.OrderItemModel, ctx.OutboxModel)
	go timeoutScanner.Run(timeoutScannerCtx)

	// 注册MQ消费者
	if err := consumer.Init(c); err != nil {
		logx.Errorw("init consumer error", logx.Field("err", err))
		panic(err)
	}

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}

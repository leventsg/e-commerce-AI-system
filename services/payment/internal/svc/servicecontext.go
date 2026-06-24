package svc

import (
	commonmq "github.com/leventsg/e-commerce-AI-system/common/mq"
	commonoutbox "github.com/leventsg/e-commerce-AI-system/common/outbox"
	"github.com/leventsg/e-commerce-AI-system/dal/model/payment"
	"github.com/leventsg/e-commerce-AI-system/services/order/orderservice"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/payment/internal/mq"
	"github.com/smartwalle/alipay/v3"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"time"
)

type ServiceContext struct {
	Config             config.Config
	Rdb                *redis.Redis
	PaymentModel       payment.PaymentsModel
	PaymentOutboxModel payment.PaymentOutboxMessagesModel
	OrderRpc           orderservice.OrderService
	Alipay             *alipay.Client
	PaymentMQ          *mq.PaymentDelayMQ
	Producer           commonmq.Producer
	Outbox             *commonoutbox.Dispatcher
	Model              sqlx.SqlConn
}

func NewServiceContext(c config.Config) *ServiceContext {
	producer, err := commonmq.NewKafkaProducer(c.KafkaMQ)
	if err != nil {
		logx.Error(err)
		panic(err)
	}

	mysql := sqlx.NewMysql(c.MysqlConfig.DataSource)
	paymentModel := payment.NewPaymentsModel(mysql)
	outboxModel := payment.NewPaymentOutboxMessagesModel(mysql)

	delayMQ, err := mq.Init(c, mysql, paymentModel, outboxModel)
	if err != nil {
		logx.Errorw("创建延迟队列失败", logx.LogField{Key: "err", Value: err})
		panic(err)
	}
	// 1. 创建支付宝客户端
	client, err := alipay.New(c.Alipay.AppId, c.Alipay.PrivateKey, false)
	if err != nil {
		logx.Errorw("创建支付宝客户端失败", logx.LogField{Key: "err", Value: err})
		panic(err)
	}
	// 2. 加载支付宝公钥用于验签
	if err := client.LoadAliPayPublicKey(c.Alipay.AlipayPublicKey); err != nil {
		logx.Errorw("加载支付宝公钥失败", logx.LogField{Key: "err", Value: err})
		panic(err)
	}

	var dispatcher *commonoutbox.Dispatcher
	if c.Outbox.Enabled {
		dispatcher = commonoutbox.NewDispatcher(commonoutbox.Config{
			BatchSize:    c.Outbox.BatchSize,
			ScanInterval: time.Duration(c.Outbox.ScanIntervalSeconds) * time.Second,
			LockTTL:      time.Duration(c.Outbox.LockTTLSeconds) * time.Second,
			RetryBase:    time.Second,
		}, outboxModel, producer)
	}

	return &ServiceContext{
		Config:             c,
		Rdb:                redis.MustNewRedis(c.RedisConf),
		PaymentModel:       paymentModel,
		PaymentOutboxModel: outboxModel,
		OrderRpc:           orderservice.NewOrderService(zrpc.MustNewClient(c.OrderRpc)),
		Alipay:             client,
		PaymentMQ:          delayMQ,
		Producer:           producer,
		Outbox:             dispatcher,
		Model:              mysql,
	}
}

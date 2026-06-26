package svc

import (
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/mq"
	commonoutbox "github.com/leventsg/e-commerce-AI-system/common/outbox"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config         config.Config
	OrderModel     order.OrdersModel
	OrderItemModel order.OrderItemsModel
	OrderAddress   order.OrderAddressesModel
	CheckoutRpc    checkoutservice.CheckoutService
	CouponRpc      coupons.CouponsClient
	UserRpc        users.UsersClient
	InventoryRpc   inventory.InventoryClient
	Model          sqlx.SqlConn
	RedisClient    *redis.Redis
	Producer       mq.Producer
	OutboxModel    order.OutboxMessagesModel
	Outbox         *commonoutbox.Dispatcher
}

func NewServiceContext(c config.Config) *ServiceContext {
	producer, err := mq.NewKafkaProducer(c.KafkaMQ)
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	mysql := sqlx.NewMysql(c.MysqlConfig.DataSource)
	outboxModel := order.NewOutboxMessagesModel(mysql)
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
		Config:         c,
		OrderModel:     order.NewOrdersModel(mysql),
		OrderItemModel: order.NewOrderItemsModel(mysql),
		OrderAddress:   order.NewOrderAddressesModel(mysql),
		Model:          mysql,
		CheckoutRpc:    checkoutservice.NewCheckoutService(zrpc.MustNewClient(c.CheckoutRpc)),
		CouponRpc:      couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponRpc)),
		UserRpc:        usersclient.NewUsers(zrpc.MustNewClient(c.UserRpc)),
		InventoryRpc:   inventoryclient.NewInventory(zrpc.MustNewClient(c.InventoryRpc)),
		RedisClient:    redis.MustNewRedis(c.RedisConf),
		Producer:       producer,
		OutboxModel:    outboxModel,
		Outbox:         dispatcher,
	}
}

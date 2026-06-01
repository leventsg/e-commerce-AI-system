package svc

import (
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/dal/model/order"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkoutservice"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/mq/delay"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/mq/notify"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"
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
	OrderDelayMQ   *delay.OrderDelayMQ
	OrderNotifyMQ  *notify.OrderNotifyMQ
}

func NewServiceContext(c config.Config) *ServiceContext {
	orderDelayMQ, err := delay.Init(c)
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	notifyMQ, err := notify.Init(c)
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	return &ServiceContext{
		Config:         c,
		OrderModel:     order.NewOrdersModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		OrderItemModel: order.NewOrderItemsModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		OrderAddress:   order.NewOrderAddressesModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		Model:          sqlx.NewMysql(c.MysqlConfig.DataSource),
		CheckoutRpc:    checkoutservice.NewCheckoutService(zrpc.MustNewClient(c.CheckoutRpc)),
		CouponRpc:      couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponRpc)),
		UserRpc:        usersclient.NewUsers(zrpc.MustNewClient(c.UserRpc)),
		InventoryRpc:   inventoryclient.NewInventory(zrpc.MustNewClient(c.InventoryRpc)),
		OrderDelayMQ:   orderDelayMQ,
		OrderNotifyMQ:  notifyMQ,
	}
}

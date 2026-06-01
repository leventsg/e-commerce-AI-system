package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/dal/model/cart"
	"github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/db"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventoryclient"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
)

type ServiceContext struct {
	Config             config.Config
	Mysql              sqlx.SqlConn
	RedisClient        *redis.Redis
	CheckoutModel      checkout.CheckoutsModel
	CheckoutItemsModel checkout.CheckoutItemsModel
	CartsModel         cart.CartsModel
	InventoryRpc       inventoryclient.Inventory
	CouponsRpc         couponsclient.Coupons
	ProductRpc         productcatalogservice.ProductCatalogService
}

func NewServiceContext(c config.Config) *ServiceContext {
	mysql := db.NewMysql(c.MysqlConfig)
	return &ServiceContext{
		Config:             c,
		Mysql:              mysql,
		RedisClient:        redis.MustNewRedis(c.RedisConf),
		CartsModel:         cart.NewCartsModel(mysql),
		CheckoutModel:      checkout.NewCheckoutsModel(mysql),
		CheckoutItemsModel: checkout.NewCheckoutItemsModel(mysql),
		InventoryRpc:       inventoryclient.NewInventory(zrpc.MustNewClient(c.InventoryRpc)),
		CouponsRpc:         couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponsRpc)),
		ProductRpc:         productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc)),
	}
}

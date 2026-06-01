package svc

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/dal/model/coupons/coupon"
	"github.com/leventsg/e-commerce-AI-system/dal/model/coupons/coupon_usage"
	"github.com/leventsg/e-commerce-AI-system/dal/model/coupons/user_coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
)

type ServiceContext struct {
	Config           config.Config
	CouponsModel     coupon.CouponsModel
	UserCouponsModel user_coupons.UserCouponsModel
	CouponUsageModel coupon_usage.CouponUsageModel
	Model            sqlx.SqlConn
	Rdb              *redis.Redis
	ProductRpc       productcatalogservice.ProductCatalogService
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:           c,
		CouponsModel:     coupon.NewCouponsModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		UserCouponsModel: user_coupons.NewUserCouponsModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		CouponUsageModel: coupon_usage.NewCouponUsageModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		Model:            sqlx.NewMysql(c.MysqlConfig.DataSource),
		Rdb:              redis.MustNewRedis(c.RedisConf),
		ProductRpc:       productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc)),
	}
}

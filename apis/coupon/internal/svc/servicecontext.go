package svc

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/config"
	"github.com/leventsg/e-commerce-AI-system/common/middleware"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
)

type ServiceContext struct {
	Config                config.Config
	CouponRpc             couponsclient.Coupons
	WithClientMiddleware  rest.Middleware
	WrapperAuthMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:                c,
		CouponRpc:             couponsclient.NewCoupons(zrpc.MustNewClient(c.CouponRpc)),
		WithClientMiddleware:  middleware.WithClientMiddleware,
		WrapperAuthMiddleware: middleware.WrapperAuthMiddleware(c.AuthsRpc, c.WhitePathList, c.OptionPathList),
	}
}

package svc

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/apis/payment/internal/config"
	"github.com/leventsg/e-commerce-AI-system/common/middleware"
	"github.com/leventsg/e-commerce-AI-system/services/payment/payment"
	"github.com/leventsg/e-commerce-AI-system/services/payment/paymentclient"
)

type ServiceContext struct {
	Config                config.Config
	WithClientMiddleware  rest.Middleware
	WrapperAuthMiddleware rest.Middleware
	PaymentRpc            payment.PaymentClient
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:                c,
		WithClientMiddleware:  middleware.WithClientMiddleware,
		WrapperAuthMiddleware: middleware.WrapperAuthMiddleware(c.AuthsRpc, nil, nil),
		PaymentRpc:            paymentclient.NewPayment(zrpc.MustNewClient(c.PaymentRpc)),
	}
}

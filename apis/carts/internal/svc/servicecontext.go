package svc

import (
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/leventsg/e-commerce-AI-system/apis/carts/internal/config"
	"github.com/leventsg/e-commerce-AI-system/common/middleware"
	"github.com/leventsg/e-commerce-AI-system/services/carts/cartsclient"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
)

type ServiceContext struct {
	Config                config.Config
	CartsRpc              cartsclient.Cart
	ProductRpc            productcatalogservice.ProductCatalogService
	WithClientMiddleware  rest.Middleware
	WrapperAuthMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:                c,
		CartsRpc:              cartsclient.NewCart(zrpc.MustNewClient(c.CartsRpc)),
		ProductRpc:            productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc)),
		WrapperAuthMiddleware: middleware.WrapperAuthMiddleware(c.AuthsRpc, nil, nil),
		WithClientMiddleware:  middleware.WithClientMiddleware,
	}
}

package svc

import (
	"github.com/leventsg/e-commerce-AI-system/apis/product/internal/config"
	"github.com/leventsg/e-commerce-AI-system/common/middleware"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                config.Config
	ProductRpc            productcatalogservice.ProductCatalogService
	WithClientMiddleware  rest.Middleware
	WrapperAuthMiddleware rest.Middleware
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:                c,
		ProductRpc:            productcatalogservice.NewProductCatalogService(zrpc.MustNewClient(c.ProductRpc)),
		WithClientMiddleware:  middleware.WithClientMiddleware,
		WrapperAuthMiddleware: middleware.WrapperAuthMiddleware(c.AuthsRpc, nil, c.OptionPathList),
	}
}

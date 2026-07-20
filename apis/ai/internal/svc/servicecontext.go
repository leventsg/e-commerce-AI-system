package svc

import (
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/config"
	"github.com/leventsg/e-commerce-AI-system/common/middleware"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagentclient"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config                config.Config
	WithClientMiddleware  rest.Middleware
	WrapperAuthMiddleware rest.Middleware
	AiAgentRpc            aiagentclient.AiAgent
}

func NewServiceContext(c config.Config) *ServiceContext {
	return &ServiceContext{
		Config:                c,
		WithClientMiddleware:  middleware.WithClientMiddleware,
		WrapperAuthMiddleware: middleware.WrapperAuthMiddleware(c.AuthsRpc, nil, nil),
		AiAgentRpc:            aiagentclient.NewAiAgent(zrpc.MustNewClient(c.AiAgentRpc)),
	}
}

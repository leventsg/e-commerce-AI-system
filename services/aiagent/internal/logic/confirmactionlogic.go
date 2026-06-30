package logic

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ConfirmActionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewConfirmActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ConfirmActionLogic {
	return &ConfirmActionLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ConfirmActionLogic) ConfirmAction(in *aiagent.ConfirmActionRequest) (*aiagent.ConfirmActionResponse, error) {
	// todo: add your logic here and delete this line

	return &aiagent.ConfirmActionResponse{}, nil
}

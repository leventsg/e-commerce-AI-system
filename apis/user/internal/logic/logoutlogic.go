package logic

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/x/errors"
)

type LogoutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LogoutLogic) Logout(req *types.LogoutRequest) (resp *types.LogoutResponse, err error) {

	user_id := l.ctx.Value(biz.UserIDKey).(uint32)

	logoutrep, err := l.svcCtx.UserRpc.Logout(l.ctx, &usersclient.LogoutRequest{

		UserId: user_id,
	})
	if err != nil {

		l.Logger.Errorw("call rpc logout failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	} else if logoutrep.StatusMsg != "" {

		return nil, errors.New(int(logoutrep.StatusCode), logoutrep.StatusMsg)

	}

	resp = &types.LogoutResponse{
		Logout_at: logoutrep.LogoutTime,
	}

	return
}

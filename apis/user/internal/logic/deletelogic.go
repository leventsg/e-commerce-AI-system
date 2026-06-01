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

type DeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteLogic {
	return &DeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteLogic) Delete(req *types.DeleteRequest) (resp *types.DeleteResponse, err error) {

	user_id := l.ctx.Value(biz.UserIDKey).(uint32)
	user_ip := l.ctx.Value(biz.ClientIPKey).(string)

	deleteresp, err := l.svcCtx.UserRpc.DeleteUser(l.ctx, &usersclient.DeleteUserRequest{

		UserId: uint32(user_id),
		Ip:     user_ip,
	})
	if err != nil {

		l.Logger.Errorw("call rpc deleteuser failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	} else if deleteresp.StatusMsg != "" {

		return nil, errors.New(int(deleteresp.StatusCode), deleteresp.StatusMsg)

	}
	resp = &types.DeleteResponse{}

	return
}

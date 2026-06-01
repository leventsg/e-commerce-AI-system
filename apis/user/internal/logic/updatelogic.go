package logic

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/x/errors"
)

type UpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateLogic {
	return &UpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateLogic) Update(req *types.UpdateRequest) (resp *types.UpdateResponse, err error) {

	if req.UserName == "" && req.Avatar == "" {
		return nil, errors.New(code.Fail, "用户名和头像不能都空")
	}

	user_id := l.ctx.Value(biz.UserIDKey).(uint32)
	user_ip := l.ctx.Value(biz.ClientIPKey).(string)

	updateresp, err := l.svcCtx.UserRpc.UpdateUser(l.ctx, &users.UpdateUserRequest{
		Ip:        user_ip,
		UserId:    user_id,
		UsrName:   req.UserName,
		AvatarUrl: req.Avatar,
	})

	if err != nil {

		l.Logger.Errorw("call rpc update failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	} else if updateresp.StatusMsg != "" {

		return nil, errors.New(int(updateresp.StatusCode), updateresp.StatusMsg)

	}

	resp = &types.UpdateResponse{

		UserName: updateresp.UserName,
		UserId:   int64(updateresp.UserId),
		Avatar:   updateresp.AvatarUrl,
	}

	return
}

package logic

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/users"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/x/errors"
)

type GetInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetInfoLogic {
	return &GetInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetInfoLogic) GetInfo(req *types.GetInfoRequest) (resp *types.GetInfoResponse, err error) {

	user_id := l.ctx.Value(biz.UserIDKey).(uint32)

	getresp, err := l.svcCtx.UserRpc.GetUser(l.ctx, &users.GetUserRequest{
		UserId: user_id,
	})
	if err != nil {

		l.Logger.Errorw("call rpc getuser failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	} else if getresp.StatusMsg != "" {

		return nil, errors.New(int(getresp.StatusCode), getresp.StatusMsg)

	}
	resp = &types.GetInfoResponse{
		UserId:    int64(getresp.UserId),
		LogoutAt:  getresp.LogoutAt,
		CreatedAt: getresp.CreatedAt,
		UpdateAt:  getresp.UpdatedAt,
		Email:     getresp.Email,
		UserName:  getresp.UserName,
		Avatar:    getresp.AvatarUrl,
	}
	fmt.Println("resp:", resp)

	return resp, nil
}

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

type DeleteAddressLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteAddressLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAddressLogic {
	return &DeleteAddressLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteAddressLogic) DeleteAddress(req *types.DeleteAddressRequest) (resp *types.DeleteAddressResponse, err error) {
	user_id := l.ctx.Value(biz.UserIDKey).(uint32)
	user_ip := l.ctx.Value(biz.ClientIPKey).(string)
	DeleteAddResp, err := l.svcCtx.UserRpc.DeleteAddress(l.ctx, &users.DeleteAddressRequest{
		Ip:        user_ip,
		UserId:    user_id,
		AddressId: req.AddressID,
	})

	if err != nil {
		l.Logger.Errorw("调用 rpc 删除地址失败", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	} else if DeleteAddResp.StatusMsg != "" {

		return nil, errors.New(int(DeleteAddResp.StatusCode), DeleteAddResp.StatusMsg)

	}

	return
}

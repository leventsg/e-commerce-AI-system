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

type PasswordCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPasswordCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PasswordCodeLogic {
	return &PasswordCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PasswordCodeLogic) PasswordCode(req *types.PasswordCodeRequest) (resp *types.PasswordCodeResponse, err error) {
	userID, _ := l.ctx.Value(biz.UserIDKey).(uint32)
	if userID == 0 {
		return nil, errors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	userIP, _ := l.ctx.Value(biz.ClientIPKey).(string)
	rpcResp, err := l.svcCtx.UserRpc.SendEmailCode(l.ctx, &usersclient.EmailCodeRequest{
		UserId: userID,
		Scene:  "update_password",
		Ip:     userIP,
	})
	if err != nil {
		l.Logger.Errorw("call rpc send update password code failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp == nil {
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp.StatusCode != code.Success {
		return nil, errors.New(int(rpcResp.StatusCode), rpcResp.StatusMsg)
	}

	return &types.PasswordCodeResponse{}, nil
}

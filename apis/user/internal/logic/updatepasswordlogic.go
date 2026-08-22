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

type UpdatePasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdatePasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdatePasswordLogic {
	return &UpdatePasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdatePasswordLogic) UpdatePassword(req *types.UpdatePasswordRequest) (resp *types.UpdatePasswordResponse, err error) {
	if req.Code == "" || req.Password == "" || req.ConfirmPassword == "" {
		return nil, errors.New(code.LoginMessageEmpty, code.LoginMessageEmptyMsg)
	}
	if req.Password != req.ConfirmPassword {
		return nil, errors.New(code.PasswordNotMatch, code.PasswordNotMatchMsg)
	}
	userID, _ := l.ctx.Value(biz.UserIDKey).(uint32)
	if userID == 0 {
		return nil, errors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	userIP, _ := l.ctx.Value(biz.ClientIPKey).(string)
	rpcResp, err := l.svcCtx.UserRpc.ResetPassword(l.ctx, &usersclient.ResetPasswordRequest{
		UserId:          userID,
		Code:            req.Code,
		Password:        req.Password,
		ConfirmPassword: req.ConfirmPassword,
		Scene:           "update_password",
		Ip:              userIP,
	})
	if err != nil {
		l.Logger.Errorw("call rpc update password failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp == nil {
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp.StatusCode != code.Success {
		return nil, errors.New(int(rpcResp.StatusCode), rpcResp.StatusMsg)
	}

	return &types.UpdatePasswordResponse{}, nil
}

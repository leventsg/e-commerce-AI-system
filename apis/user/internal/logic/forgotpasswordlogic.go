package logic

import (
	"context"
	"regexp"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/user/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/x/errors"
)

type ForgotPasswordLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewForgotPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ForgotPasswordLogic {
	return &ForgotPasswordLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ForgotPasswordLogic) ForgotPassword(req *types.ForgotPasswordRequest) (resp *types.ForgotPasswordResponse, err error) {
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Code == "" || req.Password == "" || req.ConfirmPassword == "" {
		return nil, errors.New(code.LoginMessageEmpty, code.LoginMessageEmptyMsg)
	}
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return nil, errors.New(code.EmailFormatError, code.EmailFormatErrorMsg)
	}
	if req.Password != req.ConfirmPassword {
		return nil, errors.New(code.PasswordNotMatch, code.PasswordNotMatchMsg)
	}
	userIP, _ := l.ctx.Value(biz.ClientIPKey).(string)
	rpcResp, err := l.svcCtx.UserRpc.ResetPassword(l.ctx, &usersclient.ResetPasswordRequest{
		Email:           email,
		Code:            req.Code,
		Password:        req.Password,
		ConfirmPassword: req.ConfirmPassword,
		Scene:           "forgot_password",
		Ip:              userIP,
	})
	if err != nil {
		l.Logger.Errorw("call rpc forgot password failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp == nil {
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp.StatusCode != code.Success {
		return nil, errors.New(int(rpcResp.StatusCode), rpcResp.StatusMsg)
	}

	return &types.ForgotPasswordResponse{}, nil
}

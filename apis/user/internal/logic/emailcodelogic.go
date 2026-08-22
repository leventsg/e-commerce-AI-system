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

type EmailCodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewEmailCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EmailCodeLogic {
	return &EmailCodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *EmailCodeLogic) EmailCode(req *types.EmailCodeRequest) (resp *types.EmailCodeResponse, err error) {
	scene := strings.TrimSpace(req.Scene)
	if scene == "" {
		scene = "register"
	}
	if scene != "register" && scene != "forgot_password" {
		return nil, errors.New(code.Fail, code.FailMsg)
	}
	email := strings.TrimSpace(req.Email)
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return nil, errors.New(code.EmailFormatError, code.EmailFormatErrorMsg)
	}
	userIP, _ := l.ctx.Value(biz.ClientIPKey).(string)
	rpcResp, err := l.svcCtx.UserRpc.SendEmailCode(l.ctx, &usersclient.EmailCodeRequest{
		Email: email,
		Scene: scene,
		Ip:    userIP,
	})
	if err != nil {
		l.Logger.Errorw("call rpc send email code failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp == nil {
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if rpcResp.StatusCode != code.Success {
		return nil, errors.New(int(rpcResp.StatusCode), rpcResp.StatusMsg)
	}

	return &types.EmailCodeResponse{}, nil
}

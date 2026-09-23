package logic

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	xerrors "github.com/zeromicro/x/errors"

	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type TokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TokenLogic {
	return &TokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TokenLogic) Token(req *types.CheckoutTokenReq) (resp *types.CheckoutTokenResp, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok || userID == 0 {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	result, err := l.svcCtx.CheckoutRpc.IssueCheckoutToken(l.ctx, &checkout.IssueCheckoutTokenReq{UserId: userID, IntentId: req.IntentID})
	if err != nil || result == nil {
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if result.StatusCode != code.Success {
		return nil, xerrors.New(int(result.StatusCode), result.StatusMsg)
	}
	return &types.CheckoutTokenResp{CheckoutToken: result.CheckoutToken, ExpiresAt: result.ExpiresAt}, nil
}

package logic

import (
	"context"
	xerrors "github.com/zeromicro/x/errors"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"

	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/checkout/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type DetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DetailLogic {
	return &DetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DetailLogic) Detail(req *types.CheckoutDetailReq) (resp *types.CheckoutDetailResp, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}

	res, err := l.svcCtx.CheckoutRpc.GetCheckoutDetail(l.ctx, &checkout.CheckoutDetailReq{
		PreOrderId: req.PreOrderID,
		UserId:     int32(userID),
	})
	if err != nil {
		l.Logger.Errorw("call rpc GetOrder failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if res.StatusCode != code.Success {
		return nil, xerrors.New(int(res.StatusCode), res.StatusMsg)
	}
	resp = &types.CheckoutDetailResp{
		Data: convertCheckout2Resp(res.Data),
	}
	return
}

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

type ListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListLogic {
	return &ListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListLogic) List(req *types.CheckoutListReq) (resp *types.CheckoutListResp, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	if req.PageSize <= 0 || req.PageSize > biz.MaxPageSize {
		req.PageSize = biz.MaxPageSize
	}
	res, err := l.svcCtx.CheckoutRpc.GetCheckoutList(l.ctx, &checkout.CheckoutListReq{
		UserId:   userID,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		l.Logger.Errorw("call rpc GetOrder failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if res.StatusCode != code.Success {
		return nil, xerrors.New(int(res.StatusCode), res.StatusMsg)
	}
	resp = &types.CheckoutListResp{
		Data:  make([]types.CheckoutOrder, len(res.Data)),
		Total: res.Total,
	}
	for i, item := range res.Data {
		resp.Data[i] = convertCheckout2Resp(item)
	}
	return
}

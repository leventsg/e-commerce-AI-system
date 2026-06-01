package logic

import (
	"context"
	xerrors "github.com/zeromicro/x/errors"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"

	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListOrdersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListOrdersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListOrdersLogic {
	return &ListOrdersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListOrdersLogic) ListOrders(req *types.ListOrdersReq) (resp *types.ListOrdersResp, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	res, err := l.svcCtx.OrderRpc.ListOrders(l.ctx, &order.ListOrdersRequest{
		Pagination: &order.ListOrdersRequest_Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		},
		UserId: userID,
	})
	if err != nil {
		l.Logger.Errorw("call rpc GetOrder failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if res.StatusCode != code.Success {
		return nil, xerrors.New(int(res.StatusCode), res.StatusMsg)
	}
	resp = &types.ListOrdersResp{}
	resp.Orders = make([]types.OrderResp, len(res.Orders))
	for i, item := range res.Orders {
		resp.Orders[i] = convertOrder2Resp(item)
	}

	return
}

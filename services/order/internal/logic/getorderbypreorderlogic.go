package logic

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOrderByPreOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetOrderByPreOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderByPreOrderLogic {
	return &GetOrderByPreOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetOrderByPreOrderLogic) GetOrderByPreOrder(in *order.GetOrderByPreOrderRequest) (*order.OrderDetailResponse, error) {
	res := &order.OrderDetailResponse{}
	if in.PreOrderId == "" || in.UserId == 0 {
		res.StatusCode = code.OrderParameterInvalid
		res.StatusMsg = code.OrderParameterInvalidMsg
		return res, nil
	}

	orderID, err := l.svcCtx.OrderModel.GetOrderIDByPreID(l.ctx, in.PreOrderId, int32(in.UserId))
	if err != nil {
		l.Logger.Errorw("get order id by pre order failed",
			logx.Field("err", err),
			logx.Field("pre_order_id", in.PreOrderId),
			logx.Field("user_id", in.UserId))
		return nil, err
	}
	if orderID == "" {
		res.StatusCode = code.OrderNotExist
		res.StatusMsg = code.OrderNotExistMsg
		return res, nil
	}

	return NewGetOrderLogic(l.ctx, l.svcCtx).GetOrder(getOrderRequestByPreOrderID(orderID, in))
}

func getOrderRequestByPreOrderID(orderID string, in *order.GetOrderByPreOrderRequest) *order.GetOrderRequest {
	return &order.GetOrderRequest{
		OrderId: orderID,
		UserId:  in.UserId,
	}
}

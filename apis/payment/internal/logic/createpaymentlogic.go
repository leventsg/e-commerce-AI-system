package logic

import (
	"context"
	xerrors "github.com/zeromicro/x/errors"
	"github.com/leventsg/e-commerce-AI-system/apis/payment/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/payment/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/payment/payment"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreatePaymentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreatePaymentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePaymentLogic {
	return &CreatePaymentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreatePaymentLogic) CreatePayment(req *types.PaymentReq) (resp *types.PaymentResponse, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}
	res, err := l.svcCtx.PaymentRpc.CreatePayment(l.ctx, &payment.PaymentReq{
		UserId:        userID,
		OrderId:       req.OrderID,
		PaymentMethod: payment.PaymentMethod(req.PaymentMethod),
	})
	if err != nil {
		l.Logger.Errorw("call rpc CreatePayment failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if res.StatusCode != code.Success {
		return nil, xerrors.New(int(res.StatusCode), res.StatusMsg)
	}
	resp = &types.PaymentResponse{
		Data: types.PaymentItem{
			PaymentID:     res.Payment.PaymentId,
			OrderID:       res.Payment.OrderId,
			PaidAmount:    res.Payment.PaidAmount,
			PayURL:        res.Payment.PayUrl,
			PaymentMethod: int32(res.Payment.PaymentMethod),
			Status:        int32(res.Payment.Status),
			TransactionID: res.Payment.TransactionId,
			CreatedAt:     res.Payment.CreatedAt,
		},
	}
	return
}

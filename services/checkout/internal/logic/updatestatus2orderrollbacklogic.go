package logic

import (
	"context"
	"errors"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateStatus2OrderRollbackLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateStatus2OrderRollbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateStatus2OrderRollbackLogic {
	return &UpdateStatus2OrderRollbackLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateStatus2OrderRollback 补偿操作
func (l *UpdateStatus2OrderRollbackLogic) UpdateStatus2OrderRollback(in *checkout.UpdateStatusReq) (*checkout.EmptyResp, error) {
	res := &checkout.EmptyResp{}
	err := l.svcCtx.Mysql.Transact(func(session sqlx.Session) error {
		checkoutRecord, err := l.svcCtx.CheckoutModel.FindOneByUserIdAndPreOrderIdWithSession(l.ctx, session, in.UserId, in.PreOrderId)
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				res.StatusCode = code.CheckoutRecordNotFound
				res.StatusMsg = code.CheckoutRecordNotFoundMsg
				return nil
			}
			return err
		}
		if checkoutRecord.Status == int64(checkout.CheckoutStatus_RESERVING) {
			res.StatusCode = code.CheckoutRecordStatusNotReserving
			res.StatusMsg = code.CheckoutRecordStatusNotReservingMsg
			return nil
		}

		if err = l.svcCtx.CheckoutModel.UpdateStatusWithSession(l.ctx, session,
			int64(checkout.CheckoutStatus_RESERVING), in.UserId, in.PreOrderId); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		l.Logger.Errorw("事务处理失败",
			logx.Field("err", err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	if res.StatusCode != code.Success {
		return res, nil
	}
	return res, nil
}

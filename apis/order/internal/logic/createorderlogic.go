package logic

import (
	"context"
	"github.com/dtm-labs/client/dtmgrpc"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	xerrors "github.com/zeromicro/x/errors"
	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/order/order"
)

type CreateOrderLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrderLogic {
	return &CreateOrderLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateOrderLogic) CreateOrder(req *types.CreateOrderReq) (resp *types.OrderDetailResp, err error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok {
		return nil, xerrors.New(code.AuthBlank, code.AuthBlankMsg)
	}

	orderTarget, err := l.svcCtx.Config.OrderRpc.BuildTarget()
	if err != nil {
		l.Logger.Errorw("build order rpc target failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	checkoutTarget, err := l.svcCtx.Config.CheckoutRpc.BuildTarget()
	if err != nil {
		l.Logger.Errorw("build checkout rpc target failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	couponTarget, err := l.svcCtx.Config.CouponsRpc.BuildTarget()
	if err != nil {
		l.Logger.Errorw("build coupon rpc target failed", logx.Field("err", err))
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	// --------------- saga ---------------
	sagaGrpc := dtmgrpc.NewSagaGrpc(l.svcCtx.Config.DtmRpc.Target, uuid.New().String())
	if req.CouponID != "" {
		// 锁定优惠券
		sagaGrpc.Add(couponTarget+coupons.Coupons_LockCoupon_FullMethodName,
			couponTarget+coupons.Coupons_ReleaseCoupon_FullMethodName, &coupons.LockCouponReq{
				UserId:       int32(userID),
				UserCouponId: req.CouponID,
				PreOrderId:   req.PreOrderID,
			})
	}
	// 锁定结算，进入结算确认状态
	sagaGrpc.Add(checkoutTarget+checkout.CheckoutService_UpdateStatus2Order_FullMethodName,
		checkoutTarget+checkout.CheckoutService_UpdateStatus2OrderRollback_FullMethodName, &checkout.UpdateStatusReq{
			UserId:     int32(userID),
			PreOrderId: req.PreOrderID,
		}).
		// 创建订单
		Add(orderTarget+order.OrderService_CreateOrder_FullMethodName,
			orderTarget+order.OrderService_CreateOrderRollback_FullMethodName, &order.CreateOrderRequest{
				UserId:        userID,
				PreOrderId:    req.PreOrderID,
				PaymentMethod: order.PaymentMethod_ALIPAY,
				AddressId:     req.AddressID,
				CouponId:      req.CouponID,
			})
	sagaGrpc.WithGlobalTransRequestTimeout(5000)
	sagaGrpc.WaitResult = true // 等待结果
	if err := sagaGrpc.Submit(); err != nil {
		l.Logger.Errorw("call rpc Submit failed", logx.Field("err", err))
		return nil, xerrors.New(code.CreateOrderFailed, code.CreateOrderFailedMsg)
	}
	return
}

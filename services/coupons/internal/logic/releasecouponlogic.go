package logic

import (
	"context"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/svc"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReleaseCouponLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

type releaseCouponAction int

const (
	releaseCouponActionInvalid releaseCouponAction = iota
	releaseCouponActionSkip
	releaseCouponActionUpdate
)

func releaseCouponActionForStatus(status coupons.CouponStatus) (releaseCouponAction, bool) {
	switch status {
	case coupons.CouponStatus_COUPON_STATUS_LOCKED:
		return releaseCouponActionUpdate, true
	case coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED:
		return releaseCouponActionSkip, true
	default:
		return releaseCouponActionInvalid, false
	}
}

func NewReleaseCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReleaseCouponLogic {
	return &ReleaseCouponLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ReleaseCoupon 释放优惠券（Saga补偿操作）
func (l *ReleaseCouponLogic) ReleaseCoupon(in *coupons.ReleaseCouponReq) (*coupons.EmptyResp, error) {

	res := &coupons.EmptyResp{}
	// --------------- check ---------------
	if in.UserId == 0 || len(in.UserCouponId) == 0 || len(in.PreOrderId) == 0 {
		res.StatusCode = code.NotWithParam
		res.StatusMsg = code.NotWithParamMsg
		return nil, status.Error(codes.Aborted, code.NotWithParamMsg)
	}
	// --------------- 事务操作 ---------------
	if err := l.svcCtx.Model.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
		// 1. 获取并锁定用户优惠券记录
		userCoupon, err := l.svcCtx.UserCouponsModel.GetUserCouponByUserIdCouponIdWithLock(l.ctx, session, uint64(in.UserId), in.UserCouponId)
		if err != nil {
			l.Logger.Errorw("check lock status failed", logx.Field("error", err))
			return err
		}

		// 2. 状态校验（幂等性保障）
		action, ok := releaseCouponActionForStatus(coupons.CouponStatus(userCoupon.Status))
		if !ok {
			l.Logger.Infow("coupon status is not locked", logx.Field("userId", in.UserId), logx.Field("couponId", in.UserCouponId))
			res.StatusCode = code.CouponStatusInvalid
			res.StatusMsg = code.CouponStatusInvalidMsg
			return nil
		}
		if action == releaseCouponActionSkip {
			l.Logger.Infow("coupon already released", logx.Field("userId", in.UserId), logx.Field("couponId", in.UserCouponId))
			return nil
		}

		// 3. 执行状态更新
		if err := l.svcCtx.UserCouponsModel.ReleaseUserCoupon(
			l.ctx,
			session,
			userCoupon.Id,
			coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED,
		); err != nil {
			l.Logger.Errorw("update coupon status failed", logx.Field("error", err))
			return err
		}
		return nil
	}); err != nil {
		l.Logger.Errorw("transact release coupon error", logx.Field("err", err))
		return nil, status.Error(codes.Internal, code.ServerErrorMsg) // 错误已携带正确status
	}
	if res.StatusCode != code.Success {
		return nil, status.Error(codes.Aborted, res.StatusMsg)
	}
	return res, nil
}

package logic

import (
	"context"
	"errors"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"

	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCouponLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCouponLogic {
	return &GetCouponLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetCoupon 获取单个优惠券
func (l *GetCouponLogic) GetCoupon(in *coupons.GetCouponReq) (*coupons.GetCouponResp, error) {

	res := &coupons.GetCouponResp{}

	if in.Id == "" {
		res.StatusCode = code.NotWithParam
		res.StatusMsg = code.NotWithParamMsg
		return res, nil
	}

	one, err := l.svcCtx.CouponsModel.FindOne(l.ctx, in.Id)
	if err != nil {
		if errors.Is(err, sqlx.ErrNotFound) {
			res.StatusCode = code.CouponsNotExist
			res.StatusMsg = code.CouponsNotExistMsg
			return res, nil
		}
		logx.Errorw("query coupons by id error", logx.Field("err", err))
		return nil, err
	}

	// check status
	if one.Status == 0 {
		res.StatusCode = code.CouponsExpired
		res.StatusMsg = code.CouponsExpiredMsg
		return res, nil
	}
	res.Coupon = convertCoupon2Resp(one)
	return res, nil
}

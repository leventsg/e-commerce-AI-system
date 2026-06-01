package logic

import (
	"context"
	"github.com/zeromicro/x/errors"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"

	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/coupon/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetCouponLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetCouponLogic {
	return &GetCouponLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetCouponLogic) GetCoupon(req *types.CouponItemReq) (resp *types.CouponItemResp, err error) {
	res, err := l.svcCtx.CouponRpc.GetCoupon(l.ctx, &couponsclient.GetCouponReq{
		Id: req.CouponID,
	})
	if err != nil {
		if res != nil && res.StatusCode != code.Success {
			// 处理用户级别info 错误
			return nil, errors.New(int(res.StatusCode), res.StatusMsg)
		}
		l.Logger.Errorw("call rpc GetCoupon failed", logx.Field("err", err))
		return nil, errors.New(code.ServerError, code.ServerErrorMsg)
	}
	if res.StatusCode != code.Success {
		return nil, errors.New(int(res.StatusCode), res.StatusMsg)
	}
	resp = convertCoupon2Resp(res.Coupon)
	return
}

package cancel_order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/event"
	"github.com/zeromicro/go-zero/core/logx"
)

type CouponReleaser interface {
	ReleaseCoupon(ctx context.Context, in *coupons.ReleaseCouponReq) (*coupons.EmptyResp, error)
}

type CancelOrderConsumer struct {
	CouponRpc CouponReleaser
}

func NewCancelOrderConsumer(couponRpc CouponReleaser) *CancelOrderConsumer {
	return &CancelOrderConsumer{
		CouponRpc: couponRpc,
	}
}

func (co *CancelOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.CancelOrder{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if data.CouponId == "" {
		logx.Infow("cancel order has no coupon, skip release",
			logx.Field("order_id", data.OrderId),
			logx.Field("user_id", data.UserId))
		return nil
	}
	if data.OrderId == "" || data.UserId == 0 || data.PreOrderId == "" {
		return errors.New("missing required cancel order fields")
	}
	if co.CouponRpc == nil {
		return errors.New("coupon releaser is nil")
	}

	resp, err := co.CouponRpc.ReleaseCoupon(ctx, &coupons.ReleaseCouponReq{
		UserCouponId: data.CouponId,
		PreOrderId:   data.PreOrderId,
		Reason:       data.Reason,
		UserId:       data.UserId,
	})
	if err != nil {
		logx.Errorw("release coupon by cancel order failed",
			logx.Field("err", err),
			logx.Field("order_id", data.OrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("coupon_id", data.CouponId))
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		// 优惠券状态无效，记录日志但不返回错误，避免无限重试
		if resp.StatusCode == code.CouponStatusInvalid {
			logx.Infow("coupon status invalid, skip release",
				logx.Field("order_id", data.OrderId),
				logx.Field("user_id", data.UserId),
				logx.Field("coupon_id", data.CouponId))
			return nil
		}
		// 非业务状态错误，记录错误日志并返回错误，触发重试机制
		logx.Errorw("release coupon by cancel order failed with status",
			logx.Field("order_id", data.OrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("coupon_id", data.CouponId),
			logx.Field("status_code", resp.StatusCode),
			logx.Field("status_msg", resp.StatusMsg))
		return fmt.Errorf("release coupon failed: status_code=%d status_msg=%s", resp.StatusCode, resp.StatusMsg)
	}
	return nil
}

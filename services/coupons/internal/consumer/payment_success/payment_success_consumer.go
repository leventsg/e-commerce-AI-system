package payment_success

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

type CouponUser interface {
	UseCoupon(ctx context.Context, in *coupons.UseCouponReq) (*coupons.EmptyResp, error)
}

type PaymentSuccessConsumer struct {
	couponRpc CouponUser
}

func NewPaymentSuccessConsumer(couponRpc CouponUser) *PaymentSuccessConsumer {
	return &PaymentSuccessConsumer{couponRpc: couponRpc}
}

func (c *PaymentSuccessConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.PaymentSuccess{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if data.CouponId == "" {
		logx.Infow("payment success has no coupon, skip use coupon",
			logx.Field("order_id", data.OrderId),
			logx.Field("user_id", data.UserId))
		return nil
	}
	if data.OrderId == "" || data.PreOrderId == "" || data.UserId == 0 {
		return errors.New("missing required payment success fields")
	}
	if c.couponRpc == nil {
		return errors.New("coupon user is nil")
	}

	resp, err := c.couponRpc.UseCoupon(ctx, &coupons.UseCouponReq{
		UserId:         data.UserId,
		PreOrderId:     data.PreOrderId,
		OrderId:        data.OrderId,
		CouponId:       data.CouponId,
		DiscountAmount: data.DiscountAmount,
		OriginAmount:   data.OriginalAmount,
	})
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		if resp.StatusCode == code.CouponStatusInvalid {
			logx.Infow("coupon already used or status invalid, skip payment success retry",
				logx.Field("order_id", data.OrderId),
				logx.Field("user_id", data.UserId),
				logx.Field("coupon_id", data.CouponId))
			return nil
		}
		return fmt.Errorf("use coupon failed: status_code=%d status_msg=%s", resp.StatusCode, resp.StatusMsg)
	}
	return nil
}

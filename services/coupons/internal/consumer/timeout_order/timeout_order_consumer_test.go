package timeout_order

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/internal/event"
	"github.com/stretchr/testify/require"
)

type fakeCouponReleaser struct {
	requests []*coupons.ReleaseCouponReq
	resp     *coupons.EmptyResp
	err      error
}

func (f *fakeCouponReleaser) ReleaseCoupon(ctx context.Context, in *coupons.ReleaseCouponReq) (*coupons.EmptyResp, error) {
	f.requests = append(f.requests, in)
	if f.resp != nil || f.err != nil {
		return f.resp, f.err
	}
	return &coupons.EmptyResp{}, nil
}

func TestHandleSkipsMessageWithoutCoupon(t *testing.T) {
	releaser := &fakeCouponReleaser{}
	handler := NewTimeoutOrderConsumer(releaser)
	msg, err := json.Marshal(event.CancelOrder{
		OrderId:    "order-1",
		UserId:     11,
		Reason:     "user cancel",
		PreOrderId: "pre-1",
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), msg)

	require.NoError(t, err)
	require.Empty(t, releaser.requests)
}

func TestHandleRejectsInvalidMessage(t *testing.T) {
	handler := NewTimeoutOrderConsumer(&fakeCouponReleaser{})

	err := handler.Handle(context.Background(), []byte(`{"coupon_id":"coupon-1","user_id":11}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required cancel order fields")
}

func TestHandleReleasesCoupon(t *testing.T) {
	releaser := &fakeCouponReleaser{}
	handler := NewTimeoutOrderConsumer(releaser)
	msg, err := json.Marshal(event.CancelOrder{
		OrderId:    "order-1",
		UserId:     11,
		Reason:     "user cancel",
		PreOrderId: "pre-1",
		CouponId:   "coupon-1",
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), msg)

	require.NoError(t, err)
	require.Len(t, releaser.requests, 1)
	require.Equal(t, &coupons.ReleaseCouponReq{
		UserCouponId: "coupon-1",
		PreOrderId:   "pre-1",
		Reason:       "user cancel",
		UserId:       11,
	}, releaser.requests[0])
}

func TestHandleReturnsReleaseError(t *testing.T) {
	releaser := &fakeCouponReleaser{err: errors.New("release failed")}
	handler := NewTimeoutOrderConsumer(releaser)
	msg, err := json.Marshal(event.CancelOrder{
		OrderId:    "order-1",
		UserId:     11,
		Reason:     "user cancel",
		PreOrderId: "pre-1",
		CouponId:   "coupon-1",
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "release failed")
}

func TestHandleReturnsNonSuccessStatus(t *testing.T) {
	releaser := &fakeCouponReleaser{
		resp: &coupons.EmptyResp{
			StatusCode: code.CouponStatusInvalid,
			StatusMsg:  code.CouponStatusInvalidMsg,
		},
	}
	handler := NewTimeoutOrderConsumer(releaser)
	msg, err := json.Marshal(event.CancelOrder{
		OrderId:    "order-1",
		UserId:     11,
		Reason:     "user cancel",
		PreOrderId: "pre-1",
		CouponId:   "coupon-1",
	})
	require.NoError(t, err)

	err = handler.Handle(context.Background(), msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), code.CouponStatusInvalidMsg)
}

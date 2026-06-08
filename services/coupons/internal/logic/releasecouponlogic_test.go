package logic

import (
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/stretchr/testify/require"
)

func TestReleaseCouponActionByStatus(t *testing.T) {
	tests := []struct {
		name        string
		status      coupons.CouponStatus
		wantAction  releaseCouponAction
		wantSuccess bool
	}{
		{
			name:        "locked coupon should be released",
			status:      coupons.CouponStatus_COUPON_STATUS_LOCKED,
			wantAction:  releaseCouponActionUpdate,
			wantSuccess: true,
		},
		{
			name:        "already unspecified coupon should be treated as success",
			status:      coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED,
			wantAction:  releaseCouponActionSkip,
			wantSuccess: true,
		},
		{
			name:        "used coupon should be rejected",
			status:      coupons.CouponStatus_COUPON_STATUS_USED,
			wantAction:  releaseCouponActionInvalid,
			wantSuccess: false,
		},
		{
			name:        "expired coupon should be rejected",
			status:      coupons.CouponStatus_COUPON_STATUS_EXPIRED,
			wantAction:  releaseCouponActionInvalid,
			wantSuccess: false,
		},
		{
			name:        "revoked coupon should be rejected",
			status:      coupons.CouponStatus_COUPON_STATUS_REVOKED,
			wantAction:  releaseCouponActionInvalid,
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, success := releaseCouponActionForStatus(tt.status)

			require.Equal(t, tt.wantAction, action)
			require.Equal(t, tt.wantSuccess, success)
		})
	}
}

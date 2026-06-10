package logic

import (
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
)

func TestReleaseStatus(t *testing.T) {
	tests := []struct {
		name string
		in   checkout.CheckoutStatus
		want checkout.CheckoutStatus
	}{
		{
			name: "cancelled stays cancelled",
			in:   checkout.CheckoutStatus_CANCELLED,
			want: checkout.CheckoutStatus_CANCELLED,
		},
		{
			name: "default reserving expires",
			in:   checkout.CheckoutStatus_RESERVING,
			want: checkout.CheckoutStatus_EXPIRED,
		},
		{
			name: "confirmed expires",
			in:   checkout.CheckoutStatus_CONFIRMED,
			want: checkout.CheckoutStatus_EXPIRED,
		},
		{
			name: "expired stays expired",
			in:   checkout.CheckoutStatus_EXPIRED,
			want: checkout.CheckoutStatus_EXPIRED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := releaseStatus(tt.in); got != tt.want {
				t.Fatalf("releaseStatus(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

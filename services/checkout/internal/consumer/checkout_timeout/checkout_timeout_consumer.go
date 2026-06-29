package checkout_timeout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/event"
	"github.com/zeromicro/go-zero/core/logx"
)

type CheckoutReleaser interface {
	ReleaseCheckout(ctx context.Context, in *checkout.ReleaseReq) (*checkout.EmptyResp, error)
}

type TimeoutTaskRemover interface {
	RemoveCheckoutTimeoutTask(ctx context.Context, preOrderID string) error
}

type CheckoutTimeoutConsumer struct {
	checkoutRpc CheckoutReleaser
	remover     TimeoutTaskRemover
}

func NewCheckoutTimeoutConsumer(checkoutRpc CheckoutReleaser, remover TimeoutTaskRemover) *CheckoutTimeoutConsumer {
	return &CheckoutTimeoutConsumer{
		checkoutRpc: checkoutRpc,
		remover:     remover,
	}
}

func (c *CheckoutTimeoutConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.CheckoutTimeout{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if data.PreOrderId == "" || data.UserId == 0 {
		logx.Errorw("checkout timeout event missing required fields",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("source", data.Source))
		return errors.New("missing required checkout timeout fields")
	}
	if c.checkoutRpc == nil {
		return errors.New("CheckoutRpc is nil")
	}
	if c.remover == nil {
		return errors.New("timeout task remover is nil")
	}

	resp, err := c.checkoutRpc.ReleaseCheckout(ctx, &checkout.ReleaseReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Status:     checkout.CheckoutStatus_EXPIRED,
	})
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		return fmt.Errorf("checkout timeout release failed: %s", resp.StatusMsg)
	}
	return c.remover.RemoveCheckoutTimeoutTask(ctx, data.PreOrderId)
}

package cancel_order

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

type CancelOrderConsumer struct {
	CheckoutRpc CheckoutReleaser
}

func NewCancelOrderConsumer(checkoutRpc CheckoutReleaser) *CancelOrderConsumer {
	return &CancelOrderConsumer{
		CheckoutRpc: checkoutRpc,
	}
}

func (co *CancelOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.CancelOrder{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}
	if len(data.PreOrderId) == 0 || data.UserId == 0 {
		logx.Errorw("checkout cancel order event missing required fields",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId))
		return errors.New("missing required cancel order fields")
	}

	if co.CheckoutRpc == nil {
		logx.Errorw("checkout cancel order event received but CheckoutRpc is nil",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId))
		return errors.New("CheckoutRpc is nil")
	}
	resp, err := co.CheckoutRpc.ReleaseCheckout(ctx, &checkout.ReleaseReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Status:     checkout.CheckoutStatus_CANCELLED,
	})
	if err != nil {
		logx.Errorw("checkout cancel_order consumer call rpc ReleaseCheckout failed",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("err", err))
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		logx.Errorw("checkout cancel_order consumer call rpc ReleaseCheckout failed with status",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("status_code", resp.StatusCode),
			logx.Field("status_msg", resp.StatusMsg))
		return fmt.Errorf("checkout cancel_order consumer failed to cancel order: %s", resp.StatusMsg)
	}
	return nil
}

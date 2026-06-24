package timeout_order

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

type TimeoutOrderConsumer struct {
	CheckoutRpc CheckoutReleaser
}

func NewTimeoutOrderConsumer(checkoutRpc CheckoutReleaser) *TimeoutOrderConsumer {
	return &TimeoutOrderConsumer{
		CheckoutRpc: checkoutRpc,
	}
}

func (co *TimeoutOrderConsumer) Handle(ctx context.Context, msg []byte) error {
	data := event.TimeoutOrder{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return err
	}

	if len(data.PreOrderId) == 0 || data.UserId == 0 {
		logx.Errorw("checkout timeout order event missing required fields",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("source", data.Source))
		return errors.New("missing required timeout order fields")
	}

	if co.CheckoutRpc == nil {
		logx.Errorw("checkout timeout order event received but CheckoutRpc is nil",
			logx.Field("order_id", data.OrderId),
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId))
		return errors.New("CheckoutRpc is nil")
	}

	resp, err := co.CheckoutRpc.ReleaseCheckout(ctx, &checkout.ReleaseReq{
		PreOrderId: data.PreOrderId,
		UserId:     data.UserId,
		Status:     checkout.CheckoutStatus_EXPIRED,
	})
	if err != nil {
		logx.Errorw("checkout timeout_order consumer call rpc ReleaseCheckout failed",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("err", err))
		return err
	}
	if resp != nil && resp.StatusCode != code.Success {
		logx.Errorw("checkout timeout_order consumer call rpc ReleaseCheckout failed with status",
			logx.Field("pre_order_id", data.PreOrderId),
			logx.Field("user_id", data.UserId),
			logx.Field("status_code", resp.StatusCode),
			logx.Field("status_msg", resp.StatusMsg))
		return fmt.Errorf("checkout timeout_order consumer failed to release checkout: %s", resp.StatusMsg)
	}
	return nil
}

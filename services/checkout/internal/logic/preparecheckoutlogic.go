package logic

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	model "github.com/leventsg/e-commerce-AI-system/dal/model/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/checkout"
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"github.com/leventsg/e-commerce-AI-system/services/inventory/inventory"
	"github.com/leventsg/e-commerce-AI-system/services/product/product"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const prepareTimeout = 30 * time.Second

type PrepareCheckoutLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPrepareCheckoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PrepareCheckoutLogic {
	return &PrepareCheckoutLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

// normalizeCheckout uses item data only to detect changes within an intent;
// identical items with different tokens are always independent purchases.
func normalizeCheckout(in *checkout.CheckoutReq) (string, string, []*checkout.CheckoutReq_OrderItem, error) {
	if in == nil || in.UserId == 0 || in.UserId > math.MaxInt32 || len(in.CheckoutToken) != 64 || len(in.CouponId) > 36 || len(in.OrderItems) == 0 || len(in.OrderItems) > 100 {
		return "", "", nil, errors.New("invalid checkout request")
	}
	token, err := hex.DecodeString(in.CheckoutToken)
	if err != nil || hex.EncodeToString(token) != in.CheckoutToken {
		return "", "", nil, errors.New("invalid checkout token")
	}
	items := append([]*checkout.CheckoutReq_OrderItem(nil), in.OrderItems...)
	seen := make(map[int32]bool, len(items))
	for _, item := range items {
		if item == nil || item.ProductId <= 0 || item.Quantity <= 0 || seen[item.ProductId] {
			return "", "", nil, errors.New("invalid or duplicate checkout item")
		}
		seen[item.ProductId] = true
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProductId < items[j].ProductId })
	payload, _ := json.Marshal(struct {
		CouponID string                            `json:"coupon_id"`
		Items    []*checkout.CheckoutReq_OrderItem `json:"order_items"`
	}{in.CouponId, items})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), string(payload), items, nil
}

func checkoutResponse(status int32, message string) *checkout.CheckoutResp {
	return &checkout.CheckoutResp{StatusCode: status, StatusMsg: message}
}

func (l *PrepareCheckoutLogic) PrepareCheckout(in *checkout.CheckoutReq) (*checkout.CheckoutResp, error) {
	hash, request, items, err := normalizeCheckout(in)
	if err != nil {
		return checkoutResponse(code.InvalidParams, code.InvalidParamsMsg), nil
	}
	row, err := l.svcCtx.CheckoutIntents.Find(l.ctx, uint64(in.UserId), in.CheckoutToken)
	if errors.Is(err, sqlx.ErrNotFound) {
		return checkoutResponse(code.CheckoutTokenInvalid, code.CheckoutTokenInvalidMsg), nil
	}
	if err != nil {
		return nil, err
	}
	if response := l.replay(row, hash); response != nil {
		return response, nil
	}
	claimed, err := l.svcCtx.CheckoutIntents.Claim(l.ctx, row.UserId, in.CheckoutToken, hash, request, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	if !claimed {
		row, err = l.svcCtx.CheckoutIntents.Find(l.ctx, uint64(in.UserId), in.CheckoutToken)
		if err != nil {
			return nil, err
		}
		if response := l.replay(row, hash); response != nil {
			return response, nil
		}
		return checkoutResponse(code.CheckoutProcessing, code.CheckoutProcessingMsg), nil
	}
	// Finish within a bounded deadline even if the client disconnects. No request
	// may take over this execution, even after its deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), prepareTimeout)
	defer cancel()
	return l.prepare(ctx, row, in.CouponId, items)
}

func (l *PrepareCheckoutLogic) replay(row *model.Checkouts, hash string) *checkout.CheckoutResp {
	if row.CheckoutRequestHash.Valid && row.CheckoutRequestHash.String != hash {
		return checkoutResponse(code.CheckoutTokenConflict, code.CheckoutTokenConflictMsg)
	}
	switch row.PrepareState {
	case model.PrepareIssued:
		if row.TokenExpiresAt <= time.Now().Unix() {
			return checkoutResponse(code.CheckoutTokenExpired, code.CheckoutTokenExpiredMsg)
		}
		return nil
	case model.PrepareSucceeded, model.PrepareFailed:
		var response checkout.CheckoutResp
		if !row.PrepareResult.Valid || json.Unmarshal([]byte(row.PrepareResult.String), &response) != nil ||
			(row.PrepareState == model.PrepareSucceeded && (response.StatusCode != code.Success || response.PreOrderId != row.PreOrderId || response.ExpireTime == 0)) ||
			(row.PrepareState == model.PrepareFailed && response.StatusCode == code.Success) {
			return checkoutResponse(code.CheckoutOutcomeUnknown, code.CheckoutOutcomeUnknownMsg)
		}
		return &response
	case model.PrepareProcessing:
		if row.PrepareStartedAt > time.Now().Add(-prepareTimeout-5*time.Second).Unix() {
			return checkoutResponse(code.CheckoutProcessing, code.CheckoutProcessingMsg)
		}
		return l.finishFailure(row, model.PrepareUnknown, code.CheckoutOutcomeUnknown, code.CheckoutOutcomeUnknownMsg)
	default:
		return checkoutResponse(code.CheckoutOutcomeUnknown, code.CheckoutOutcomeUnknownMsg)
	}
}

func (l *PrepareCheckoutLogic) finishFailure(row *model.Checkouts, state int64, status int32, message string) *checkout.CheckoutResp {
	response := checkoutResponse(status, message)
	payload, _ := json.Marshal(response)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 3*time.Second)
	defer cancel()
	if err := l.svcCtx.CheckoutIntents.FinishFailure(ctx, row.UserId, row.PreOrderId, state, string(payload)); err != nil {
		l.Logger.Errorw("persist checkout outcome failed", logx.Field("pre_order_id", row.PreOrderId), logx.Field("err", err))
		return checkoutResponse(code.CheckoutOutcomeUnknown, code.CheckoutOutcomeUnknownMsg)
	}
	return response
}

func (l *PrepareCheckoutLogic) prepare(ctx context.Context, row *model.Checkouts, couponID string, orderItems []*checkout.CheckoutReq_OrderItem) (*checkout.CheckoutResp, error) {
	fail := func(status int32, message string) (*checkout.CheckoutResp, error) {
		return l.finishFailure(row, model.PrepareFailed, status, message), nil
	}
	unknown := func() (*checkout.CheckoutResp, error) {
		return l.finishFailure(row, model.PrepareUnknown, code.CheckoutOutcomeUnknown, code.CheckoutOutcomeUnknownMsg), nil
	}
	items := make([]*model.CheckoutItems, 0, len(orderItems))
	inventoryItems := make([]*inventory.InventoryReq_Items, 0, len(orderItems))
	couponItems := make([]*couponsclient.Items, 0, len(orderItems))
	var total int64
	for _, item := range orderItems {
		response, err := l.svcCtx.ProductRpc.GetProduct(ctx, &product.GetProductReq{Id: uint32(item.ProductId)})
		if err != nil || response == nil || response.StatusCode != code.Success || response.Product == nil {
			return fail(code.QueryOrderProductInfoFailed, code.QueryOrderProductInfoFailedMsg)
		}
		price := response.Product.Price
		if price < 0 || price > (math.MaxInt64-total)/int64(item.Quantity) {
			return fail(code.InvalidParams, code.InvalidParamsMsg)
		}
		total += price * int64(item.Quantity)
		snapshot, _ := json.Marshal(map[string]string{"name": response.Product.Name, "desc": response.Product.Description})
		items = append(items, &model.CheckoutItems{PreOrderId: row.PreOrderId, ProductId: uint64(item.ProductId), Quantity: uint64(item.Quantity), Price: price, Snapshot: string(snapshot)})
		inventoryItems = append(inventoryItems, &inventory.InventoryReq_Items{ProductId: item.ProductId, Quantity: item.Quantity})
		couponItems = append(couponItems, &couponsclient.Items{ProductId: item.ProductId, Quantity: item.Quantity})
	}
	final := total
	if couponID != "" {
		response, err := l.svcCtx.CouponsRpc.CalculateCoupon(ctx, &couponsclient.CalculateCouponReq{CouponId: couponID, UserId: int32(row.UserId), Items: couponItems})
		if err != nil || response == nil {
			return fail(code.QueryUserCouponFailed, code.QueryUserCouponFailedMsg)
		}
		if response.StatusCode != code.Success {
			return fail(int32(response.StatusCode), response.StatusMsg)
		}
		if !response.IsUsable || response.FinalAmount < 0 || response.FinalAmount > total {
			return fail(code.InvalidParams, "优惠券不可用于当前结算")
		}
		final = response.FinalAmount
	}
	row.CouponId = sql.NullString{String: couponID, Valid: couponID != ""}
	row.OriginalAmount = total
	row.FinalAmount = final
	row.ExpireTime = time.Now().Add(biz.CheckoutExpireTime).Unix()
	// Schedule before inventory side effects. Uncommitted/failed intents are not
	// usable checkouts; ambiguous reservations require manual reconciliation.
	if _, err := l.svcCtx.RedisClient.ZaddCtx(ctx, biz.CheckoutTimeoutZSetKey, row.ExpireTime, row.PreOrderId); err != nil {
		return fail(code.InternalFailed, code.InternalFailedMsg)
	}
	invResponse, err := l.svcCtx.InventoryRpc.DecreasePreInventory(ctx, &inventory.InventoryReq{UserId: int32(row.UserId), PreOrderId: row.PreOrderId, Items: inventoryItems})
	if err != nil || invResponse == nil {
		return unknown()
	}
	if invResponse.StatusCode != code.Success {
		if invResponse.StatusCode == code.InventoryNotEnough || invResponse.StatusCode == code.InvalidParams {
			return fail(invResponse.StatusCode, invResponse.StatusMsg)
		}
		return unknown()
	}
	response := &checkout.CheckoutResp{PreOrderId: row.PreOrderId, ExpireTime: row.ExpireTime, PayMethod: []int64{1, 2}}
	payload, _ := json.Marshal(response)
	if err := l.svcCtx.CheckoutIntents.Complete(ctx, row, items, string(payload)); err != nil {
		l.Logger.Errorw("checkout completion uncertain", logx.Field("pre_order_id", row.PreOrderId), logx.Field("err", err))
		// A lost COMMIT response may mean success. Never return inventory or reset
		// the token. A fresh read can recover a committed replay result.
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), 3*time.Second)
		defer cancel()
		saved, readErr := l.svcCtx.CheckoutIntents.Find(readCtx, row.UserId, row.CheckoutToken.String)
		if readErr == nil && saved.PrepareState == model.PrepareSucceeded {
			return l.replay(saved, saved.CheckoutRequestHash.String), nil
		}
		return unknown()
	}
	return response, nil
}

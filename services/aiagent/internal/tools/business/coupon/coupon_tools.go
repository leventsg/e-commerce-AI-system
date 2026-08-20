package coupon_tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/coupons"
	"github.com/leventsg/e-commerce-AI-system/services/coupons/couponsclient"
	"google.golang.org/grpc"
)

type CouponQueryRPC interface {
	ListCoupons(ctx context.Context, in *couponsclient.ListCouponsReq, opts ...grpc.CallOption) (*couponsclient.ListCouponsResp, error)
	GetCoupon(ctx context.Context, in *couponsclient.GetCouponReq, opts ...grpc.CallOption) (*couponsclient.GetCouponResp, error)
	ListUserCoupons(ctx context.Context, in *couponsclient.ListUserCouponsReq, opts ...grpc.CallOption) (*couponsclient.ListUserCouponsResp, error)
	ListCouponUsages(ctx context.Context, in *couponsclient.ListCouponUsagesReq, opts ...grpc.CallOption) (*couponsclient.ListCouponUsagesResp, error)
	CalculateCoupon(ctx context.Context, in *couponsclient.CalculateCouponReq, opts ...grpc.CallOption) (*couponsclient.CalculateCouponResp, error)
}

type CouponCalculateRPC interface {
	CalculateCoupon(ctx context.Context, in *couponsclient.CalculateCouponReq, opts ...grpc.CallOption) (*couponsclient.CalculateCouponResp, error)
}

type CouponWriteRPC interface {
	ClaimCoupon(ctx context.Context, in *couponsclient.ClaimCouponReq, opts ...grpc.CallOption) (*couponsclient.ClaimCouponResp, error)
}

func CouponQueryHandlers(rpc CouponQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolCouponList:      couponListHandler(rpc),
		domain.ToolCouponDetail:    couponDetailHandler(rpc),
		domain.ToolCouponMyList:    couponMyListHandler(rpc),
		domain.ToolCouponUsageList: couponUsageListHandler(rpc),
		domain.ToolCouponCalculate: couponCalculateHandler(rpc),
	}
}

func CouponWriteHandlers(rpc CouponWriteRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolCouponClaim: couponClaimHandler(rpc),
	}
}

// 优惠券领取工具处理函数
func couponClaimHandler(rpc CouponWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		// 解析参数
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		couponID, err := helper.RequiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		// 调用优惠券服务的领取接口
		resp, err := rpc.ClaimCoupon(ctx, &couponsclient.ClaimCouponReq{UserId: userID, CouponId: couponID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_claim rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_claim returned nil response")
		}
		if err := helper.ValidateRPCResponse("coupon_claim", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Coupon == nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_claim returned empty coupon")
		}
		return core.HandlerResult{
			Data: map[string]any{
				"coupon_id": resp.Coupon.Id,
				"name":      resp.Coupon.Name,
				"type":      resp.Coupon.Type.String(),
			},
			Summary: fmt.Sprintf("已领取优惠券“%s”。", resp.Coupon.Name),
		}, nil
	}
}

// 优惠券查询：获取可用优惠券列表
func couponListHandler(rpc CouponQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		page, pageSize, err := helper.QueryPagination(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		typeValue, err := helper.OptionalInt64Argument(req.Arguments, "type", 0)
		if err != nil {
			return core.HandlerResult{}, err
		}
		if typeValue < 0 || typeValue > int64(coupons.CouponType_COUPON_TYPE_FIXED_AMOUNT) {
			return core.HandlerResult{}, helper.InvalidArgument("type", "is not a supported coupon type")
		}
		resp, err := rpc.ListCoupons(ctx, &couponsclient.ListCouponsReq{
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
			Type:       int32(typeValue),
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_list rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_list returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("coupon_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		values := compactCoupons(resp.Coupons)
		return core.HandlerResult{
			Data: map[string]any{
				"total":     resp.TotalCount,
				"page":      page,
				"page_size": pageSize,
				"coupons":   values,
			},
			Summary: fmt.Sprintf("查询到 %d 张可用优惠券。", len(values)),
		}, nil
	}
}

// 获取优惠券详情
func couponDetailHandler(rpc CouponQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		couponID, err := helper.RequiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetCoupon(ctx, &couponsclient.GetCouponReq{Id: couponID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_detail rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_detail returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("coupon_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Coupon == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_detail returned empty coupon", helper.ErrQueryRPCUnavailable)
		}
		return core.HandlerResult{
			Data:    map[string]any{"coupon": compactCoupon(resp.Coupon)},
			Summary: fmt.Sprintf("已查询优惠券“%s”的详情。", resp.Coupon.Name),
		}, nil
	}
}

// 查看我的优惠券
func couponMyListHandler(rpc CouponQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		page, pageSize, err := helper.QueryPagination(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		statusText, err := helper.OptionalStringArgument(req.Arguments, "status")
		if err != nil {
			return core.HandlerResult{}, err
		}
		status, filterStatus, err := parseCouponStatus(statusText)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.ListUserCoupons(ctx, &couponsclient.ListUserCouponsReq{
			UserId:     userID,
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_my_list rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_my_list returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("coupon_my_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		values := make([]map[string]any, 0, len(resp.UserCoupons))
		for _, item := range resp.UserCoupons {
			if item == nil || (filterStatus && item.Status != status) {
				continue
			}
			values = append(values, compactUserCoupon(item))
		}
		total := resp.TotalCount
		if filterStatus {
			total = int32(len(values))
		}
		return core.HandlerResult{
			Data: map[string]any{
				"total":     total,
				"page":      page,
				"page_size": pageSize,
				"coupons":   values,
			},
			Summary: fmt.Sprintf("你有 %d 张符合条件的优惠券。", len(values)),
		}, nil
	}
}

// 我的优惠券使用情况
func couponUsageListHandler(rpc CouponQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		page, pageSize, err := helper.QueryPagination(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.ListCouponUsages(ctx, &couponsclient.ListCouponUsagesReq{
			UserId:     uint32(userID),
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_usage_list rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_usage_list returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("coupon_usage_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		values := compactCouponUsages(resp.Usages)
		return core.HandlerResult{
			Data: map[string]any{
				"total":     resp.TotalCount,
				"page":      page,
				"page_size": pageSize,
				"usages":    values,
			},
			Summary: fmt.Sprintf("查询到 %d 条优惠券使用记录。", len(values)),
		}, nil
	}
}

// 优惠券使用折扣后金额计算
func couponCalculateHandler(rpc CouponQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		couponID, err := helper.RequiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		items, err := couponItemsArgument(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.CalculateCoupon(ctx, &couponsclient.CalculateCouponReq{
			UserId:   userID,
			CouponId: couponID,
			Items:    items,
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("coupon_calculate rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: coupon_calculate returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("coupon_calculate", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		summary := "该优惠券不可用于当前商品。"
		if resp.IsUsable {
			summary = fmt.Sprintf("该优惠券可用，可优惠 %d 分。", resp.DiscountAmount)
		}
		return core.HandlerResult{
			Data: map[string]any{
				"coupon_id":       couponID,
				"origin_amount":   resp.OriginAmount,
				"final_amount":    resp.FinalAmount,
				"discount_amount": resp.DiscountAmount,
				"coupon_type":     resp.CouponType,
				"is_usable":       resp.IsUsable,
				"unusable_reason": resp.UnusableReason,
			},
			Summary: summary,
		}, nil
	}
}

func couponItemsArgument(args map[string]any) ([]*couponsclient.Items, error) {
	value, ok := args["items"]
	if !ok {
		return nil, helper.InvalidArgument("items", "is required")
	}
	rawItems, ok := value.([]any)
	if !ok || len(rawItems) == 0 {
		return nil, helper.InvalidArgument("items", "must be a non-empty array")
	}
	items := make([]*couponsclient.Items, 0, len(rawItems))
	for _, raw := range rawItems {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, helper.InvalidArgument("items", "must contain objects")
		}
		productIDValue, err := helper.RequiredInt64Argument(object, "product_id")
		if err != nil {
			return nil, err
		}
		quantityValue, err := helper.RequiredInt64Argument(object, "quantity")
		if err != nil {
			return nil, err
		}
		productID, err := helper.PositiveInt32(productIDValue, "product_id")
		if err != nil {
			return nil, err
		}
		quantity, err := helper.PositiveInt32(quantityValue, "quantity")
		if err != nil {
			return nil, err
		}
		items = append(items, &couponsclient.Items{ProductId: productID, Quantity: quantity})
	}
	return items, nil
}

func parseCouponStatus(value string) (coupons.CouponStatus, bool, error) {
	key := strings.ToLower(strings.TrimSpace(value))
	if key == "" {
		return coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED, false, nil
	}
	aliases := map[string]coupons.CouponStatus{
		"available": coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED,
		"unused":    coupons.CouponStatus_COUPON_STATUS_UNSPECIFIED,
		"locked":    coupons.CouponStatus_COUPON_STATUS_LOCKED,
		"used":      coupons.CouponStatus_COUPON_STATUS_USED,
		"expired":   coupons.CouponStatus_COUPON_STATUS_EXPIRED,
		"revoked":   coupons.CouponStatus_COUPON_STATUS_REVOKED,
	}
	if status, ok := aliases[key]; ok {
		return status, true, nil
	}
	protoKey := strings.ToUpper(key)
	if !strings.HasPrefix(protoKey, "COUPON_STATUS_") {
		protoKey = "COUPON_STATUS_" + protoKey
	}
	raw, ok := coupons.CouponStatus_value[protoKey]
	if !ok {
		return 0, false, helper.InvalidArgument("status", "contains an unsupported coupon status")
	}
	return coupons.CouponStatus(raw), true, nil
}

func compactCoupons(values []*couponsclient.Coupon) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, compactCoupon(value))
		}
	}
	return result
}

func compactCoupon(value *couponsclient.Coupon) map[string]any {
	return map[string]any{
		"coupon_id":       value.Id,
		"name":            value.Name,
		"type":            value.Type.String(),
		"value":           value.Value,
		"min_amount":      value.MinAmount,
		"start_time":      value.StartTime,
		"end_time":        value.EndTime,
		"remaining_count": value.RemainingCount,
	}
}

func compactUserCoupon(value *couponsclient.UserCoupon) map[string]any {
	return map[string]any{
		"user_coupon_id": value.Id,
		"coupon_id":      value.CouponId,
		"status":         value.Status.String(),
		"order_id":       value.OrderId,
		"used_at":        value.UsedAt,
		"created_at":     value.CreatedAt,
	}
}

func compactCouponUsages(values []*couponsclient.CouponUsage) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		result = append(result, map[string]any{
			"usage_id":        value.Id,
			"pre_order_id":    value.PreOrderId,
			"order_id":        value.OrderId,
			"coupon_id":       value.CouponId,
			"coupon_type":     value.CouponType.String(),
			"origin_value":    value.OriginValue,
			"discount_amount": value.DiscountAmount,
			"applied_at":      value.AppliedAt,
		})
	}
	return result
}

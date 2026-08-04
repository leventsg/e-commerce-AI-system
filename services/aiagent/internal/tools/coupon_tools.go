package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
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

func couponQueryHandlers(rpc CouponQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCouponList:      couponListHandler(rpc),
		domain.ToolCouponDetail:    couponDetailHandler(rpc),
		domain.ToolCouponMyList:    couponMyListHandler(rpc),
		domain.ToolCouponUsageList: couponUsageListHandler(rpc),
		domain.ToolCouponCalculate: couponCalculateHandler(rpc),
	}
}

func couponWriteHandlers(rpc CouponWriteRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolCouponClaim: couponClaimHandler(rpc),
	}
}

// 优惠券领取工具处理函数
func couponClaimHandler(rpc CouponWriteRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 解析参数
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		couponID, err := requiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return HandlerResult{}, err
		}
		// 调用优惠券服务的领取接口
		resp, err := rpc.ClaimCoupon(ctx, &couponsclient.ClaimCouponReq{UserId: userID, CouponId: couponID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_claim rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("coupon_claim returned nil response")
		}
		if err := validateRPCResponse("coupon_claim", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		if resp.Coupon == nil {
			return HandlerResult{}, fmt.Errorf("coupon_claim returned empty coupon")
		}
		return HandlerResult{
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
func couponListHandler(rpc CouponQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		typeValue, err := optionalInt64Argument(req.Arguments, "type", 0)
		if err != nil {
			return HandlerResult{}, err
		}
		if typeValue < 0 || typeValue > int64(coupons.CouponType_COUPON_TYPE_FIXED_AMOUNT) {
			return HandlerResult{}, invalidArgument("type", "is not a supported coupon type")
		}
		resp, err := rpc.ListCoupons(ctx, &couponsclient.ListCouponsReq{
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
			Type:       int32(typeValue),
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_list returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("coupon_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		values := compactCoupons(resp.Coupons)
		return HandlerResult{
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

func couponDetailHandler(rpc CouponQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		couponID, err := requiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.GetCoupon(ctx, &couponsclient.GetCouponReq{Id: couponID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_detail rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_detail returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("coupon_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		if resp.Coupon == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_detail returned empty coupon", ErrQueryRPCUnavailable)
		}
		return HandlerResult{
			Data:    map[string]any{"coupon": compactCoupon(resp.Coupon)},
			Summary: fmt.Sprintf("已查询优惠券“%s”的详情。", resp.Coupon.Name),
		}, nil
	}
}

func couponMyListHandler(rpc CouponQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		statusText, err := optionalStringArgument(req.Arguments, "status")
		if err != nil {
			return HandlerResult{}, err
		}
		status, filterStatus, err := parseCouponStatus(statusText)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.ListUserCoupons(ctx, &couponsclient.ListUserCouponsReq{
			UserId:     userID,
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_my_list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_my_list returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("coupon_my_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
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
		return HandlerResult{
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

func couponUsageListHandler(rpc CouponQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.ListCouponUsages(ctx, &couponsclient.ListCouponUsagesReq{
			UserId:     uint32(userID),
			Pagination: &couponsclient.PaginationReq{Page: page, Size: pageSize},
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_usage_list rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_usage_list returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("coupon_usage_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		values := compactCouponUsages(resp.Usages)
		return HandlerResult{
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

func couponCalculateHandler(rpc CouponQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		couponID, err := requiredStringArgument(req.Arguments, "coupon_id")
		if err != nil {
			return HandlerResult{}, err
		}
		items, err := couponItemsArgument(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}
		resp, err := rpc.CalculateCoupon(ctx, &couponsclient.CalculateCouponReq{
			UserId:   userID,
			CouponId: couponID,
			Items:    items,
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("coupon_calculate rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: coupon_calculate returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("coupon_calculate", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		summary := "该优惠券不可用于当前商品。"
		if resp.IsUsable {
			summary = fmt.Sprintf("该优惠券可用，可优惠 %d 分。", resp.DiscountAmount)
		}
		return HandlerResult{
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
		return nil, invalidArgument("items", "is required")
	}
	rawItems, ok := value.([]any)
	if !ok || len(rawItems) == 0 {
		return nil, invalidArgument("items", "must be a non-empty array")
	}
	items := make([]*couponsclient.Items, 0, len(rawItems))
	for _, raw := range rawItems {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, invalidArgument("items", "must contain objects")
		}
		productIDValue, err := requiredInt64Argument(object, "product_id")
		if err != nil {
			return nil, err
		}
		quantityValue, err := requiredInt64Argument(object, "quantity")
		if err != nil {
			return nil, err
		}
		productID, err := positiveInt32(productIDValue, "product_id")
		if err != nil {
			return nil, err
		}
		quantity, err := positiveInt32(quantityValue, "quantity")
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
		return 0, false, invalidArgument("status", "contains an unsupported coupon status")
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

package domain

const (
	RiskLow  = "low"
	RiskHigh = "high"
)

const (
	ToolProductSearch    = "product_search"
	ToolProductDetail    = "product_detail"
	ToolProductRecommend = "product_recommend"
	ToolInventoryGet     = "inventory_get"

	ToolOrderGet    = "order_get"
	ToolOrderList   = "order_list"
	ToolOrderCreate = "order_create"
	ToolOrderCancel = "order_cancel"

	ToolCheckoutPrepare = "checkout_prepare"
	ToolCheckoutDetail  = "checkout_detail"

	ToolCartList   = "cart_list"
	ToolCartAdd    = "cart_add"
	ToolCartSub    = "cart_sub"
	ToolCartDelete = "cart_delete"

	ToolCouponList      = "coupon_list"
	ToolCouponDetail    = "coupon_detail"
	ToolCouponClaim     = "coupon_claim"
	ToolCouponMyList    = "coupon_my_list"
	ToolCouponUsageList = "coupon_usage_list"
	ToolCouponCalculate = "coupon_calculate"
)

type Metadata struct {
	Name                string
	Risk                string
	RequireConfirmation bool
	TimeoutSeconds      int64
	WriteOperation      bool
	RPCService          string
	RPCMethod           string
}

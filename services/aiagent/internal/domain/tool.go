package domain

const (
	RiskLow  = "low"
	RiskHigh = "high"
)

const (
	ToolProductSearch    = "product.search"
	ToolProductDetail    = "product.detail"
	ToolProductRecommend = "product.recommend"
	ToolInventoryGet     = "inventory.get"

	ToolOrderGet    = "order.get"
	ToolOrderList   = "order.list"
	ToolOrderCreate = "order.create"
	ToolOrderCancel = "order.cancel"

	ToolCheckoutPrepare = "checkout.prepare"
	ToolCheckoutDetail  = "checkout.detail"

	ToolCartList   = "cart.list"
	ToolCartAdd    = "cart.add"
	ToolCartSub    = "cart.sub"
	ToolCartDelete = "cart.delete"

	ToolCouponList      = "coupon.list"
	ToolCouponDetail    = "coupon.detail"
	ToolCouponClaim     = "coupon.claim"
	ToolCouponMyList    = "coupon.my_list"
	ToolCouponUsageList = "coupon.usage_list"
	ToolCouponCalculate = "coupon.calculate"
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

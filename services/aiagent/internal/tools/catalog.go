package tools

import (
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	cart "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/cart"
	checkout "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/checkout"
	coupon "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/coupon"
	inventory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/inventory"
	order "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/order"
	product "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/business/product"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

type CartRPC interface {
	cart.CartQueryRPC
	cart.CartWriteRPC
	cart.CartHighRiskRPC
}

type OrderRPC interface {
	order.OrderQueryRPC
	order.OrderHighRiskRPC
}

type CouponRPC interface {
	coupon.CouponQueryRPC
	coupon.CouponWriteRPC
	coupon.CouponCalculateRPC
}

type CheckoutRPC interface {
	checkout.CheckoutQueryRPC
	checkout.CheckoutWriteRPC
}

type DefaultToolClients struct {
	Product         product.ProductQueryRPC
	Inventory       inventory.InventoryQueryRPC
	Order           OrderRPC
	OrderQuery      order.OrderQueryRPC
	OrderHighRisk   order.OrderHighRiskRPC
	OrderCreateAPI  order.OrderCreateAPI
	Cart            CartRPC
	CartQuery       cart.CartQueryRPC
	CartWrite       cart.CartWriteRPC
	CartHighRisk    cart.CartHighRiskRPC
	Coupon          CouponRPC
	CouponQuery     coupon.CouponQueryRPC
	CouponWrite     coupon.CouponWriteRPC
	CouponCalculate coupon.CouponCalculateRPC
	Checkout        CheckoutRPC
	CheckoutQuery   checkout.CheckoutQueryRPC
	CheckoutWrite   checkout.CheckoutWriteRPC
}

// 对工具注册表补充工具handler和确认摘要函数
func DefaultBusinessTools(clients DefaultToolClients, timeout config.ToolTimeoutConfig) []core.Tool {
	// 超时时间
	queryTimeout := timeout.QuerySeconds
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeoutSeconds
	}
	writeTimeout := timeout.WriteSeconds
	if writeTimeout <= 0 {
		writeTimeout = defaultWriteTimeoutSeconds
	}
	// 工具执行函数
	handlers := make(map[string]core.HandlerFunc)
	orderQuery := clients.OrderQuery
	orderHighRisk := clients.OrderHighRisk
	if clients.Order != nil {
		orderQuery = clients.Order
		orderHighRisk = clients.Order
	}
	cartQuery := clients.CartQuery
	cartWrite := clients.CartWrite
	cartHighRisk := clients.CartHighRisk
	if clients.Cart != nil {
		cartQuery = clients.Cart
		cartWrite = clients.Cart
		cartHighRisk = clients.Cart
	}
	couponQuery := clients.CouponQuery
	couponWrite := clients.CouponWrite
	couponCalculate := clients.CouponCalculate
	if clients.Coupon != nil {
		couponQuery = clients.Coupon
		couponWrite = clients.Coupon
		couponCalculate = clients.Coupon
	}
	checkoutQuery := clients.CheckoutQuery
	checkoutWrite := clients.CheckoutWrite
	if clients.Checkout != nil {
		checkoutQuery = clients.Checkout
		checkoutWrite = clients.Checkout
	}
	mergeHandlers(handlers, product.ProductQueryHandlers(clients.Product))
	mergeHandlers(handlers, inventory.InventoryQueryHandlers(clients.Inventory))
	mergeHandlers(handlers, order.OrderQueryHandlers(orderQuery))
	mergeHandlers(handlers, order.OrderHighRiskHandlers(clients.OrderCreateAPI, orderHighRisk))
	mergeHandlers(handlers, cart.CartQueryHandlers(cartQuery))
	mergeHandlers(handlers, cart.CartWriteHandlers(cartWrite))
	mergeHandlers(handlers, cart.CartHighRiskHandlers(cartHighRisk))
	mergeHandlers(handlers, coupon.CouponQueryHandlers(couponQuery))
	mergeHandlers(handlers, coupon.CouponWriteHandlers(couponWrite))
	mergeHandlers(handlers, checkout.CheckoutQueryHandlers(checkoutQuery))
	mergeHandlers(handlers, checkout.CheckoutWriteHandlers(checkoutWrite))

	// 高风险操作的确认摘要函数
	summaries := highRiskSummaryFuncs(clients, cartHighRisk, checkoutQuery, couponCalculate)
	// 工具metadata
	tools := defaultSchemaTools(queryTimeout, writeTimeout)
	result := make([]core.Tool, 0, len(tools))
	// 汇总：metadata + handler + confirmation summary
	for _, tool := range tools {
		tool.Handler = handlers[tool.Name]
		tool.ConfirmationSummary = summaries[tool.Name]
		result = append(result, tool)
	}
	return result
}

func highRiskSummaryFuncs(clients DefaultToolClients, cart cart.CartHighRiskRPC, checkout checkout.CheckoutQueryRPC, coupon coupon.CouponCalculateRPC) map[string]core.ConfirmationSummaryFunc {
	builder := &confirmationSummaryBuilder{
		cart:     cart,
		product:  clients.Product,
		checkout: checkout,
		coupon:   coupon,
	}
	return map[string]core.ConfirmationSummaryFunc{
		domain.ToolCartDelete:  builder.cartDeleteSummary,
		domain.ToolOrderCreate: builder.orderCreateSummary,
		domain.ToolOrderCancel: builder.orderCancelSummary,
	}
}

package tools

import (
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type CartRPC interface {
	CartQueryRPC
	CartWriteRPC
	CartHighRiskRPC
}

type OrderRPC interface {
	OrderQueryRPC
	OrderHighRiskRPC
}

type CouponRPC interface {
	CouponQueryRPC
	CouponWriteRPC
	CouponCalculateRPC
}

type CheckoutRPC interface {
	CheckoutQueryRPC
	CheckoutWriteRPC
}

type DefaultToolClients struct {
	Product         ProductQueryRPC
	Inventory       InventoryQueryRPC
	Order           OrderRPC
	OrderQuery      OrderQueryRPC
	OrderHighRisk   OrderHighRiskRPC
	Cart            CartRPC
	CartQuery       CartQueryRPC
	CartWrite       CartWriteRPC
	CartHighRisk    CartHighRiskRPC
	Coupon          CouponRPC
	CouponQuery     CouponQueryRPC
	CouponWrite     CouponWriteRPC
	CouponCalculate CouponCalculateRPC
	Checkout        CheckoutRPC
	CheckoutQuery   CheckoutQueryRPC
	CheckoutWrite   CheckoutWriteRPC
}

// 对工具注册表补充工具handler和确认摘要函数
func DefaultTools(clients DefaultToolClients, timeout config.ToolTimeoutConfig) []Tool {
	queryTimeout := timeout.QuerySeconds
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeoutSeconds
	}
	writeTimeout := timeout.WriteSeconds
	if writeTimeout <= 0 {
		writeTimeout = defaultWriteTimeoutSeconds
	}
	handlers := make(map[string]HandlerFunc)
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
	mergeHandlers(handlers, productQueryHandlers(clients.Product))
	mergeHandlers(handlers, inventoryQueryHandlers(clients.Inventory))
	mergeHandlers(handlers, orderQueryHandlers(orderQuery))
	mergeHandlers(handlers, orderHighRiskHandlers(orderHighRisk))
	mergeHandlers(handlers, cartQueryHandlers(cartQuery))
	mergeHandlers(handlers, cartWriteHandlers(cartWrite))
	mergeHandlers(handlers, cartHighRiskHandlers(cartHighRisk))
	mergeHandlers(handlers, couponQueryHandlers(couponQuery))
	mergeHandlers(handlers, couponWriteHandlers(couponWrite))
	mergeHandlers(handlers, checkoutQueryHandlers(checkoutQuery))
	mergeHandlers(handlers, checkoutWriteHandlers(checkoutWrite))

	summaries := highRiskSummaryFuncs(clients, cartHighRisk, checkoutQuery, couponCalculate)
	tools := defaultSchemaTools(queryTimeout, writeTimeout)
	result := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		tool.Handler = handlers[tool.Name]
		tool.ConfirmationSummary = summaries[tool.Name]
		result = append(result, tool)
	}
	return result
}

func highRiskSummaryFuncs(clients DefaultToolClients, cart CartHighRiskRPC, checkout CheckoutQueryRPC, coupon CouponCalculateRPC) map[string]ConfirmationSummaryFunc {
	builder := &confirmationSummaryBuilder{
		cart:     cart,
		product:  clients.Product,
		checkout: checkout,
		coupon:   coupon,
	}
	return map[string]ConfirmationSummaryFunc{
		domain.ToolCartDelete:  builder.cartDeleteSummary,
		domain.ToolOrderCreate: builder.orderCreateSummary,
		domain.ToolOrderCancel: builder.orderCancelSummary,
	}
}

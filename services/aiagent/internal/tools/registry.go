package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

const (
	defaultQueryTimeoutSeconds = int64(3)
	defaultWriteTimeoutSeconds = int64(5)
)

var (
	ErrToolNotFound              = errors.New("ai tool not found")
	ErrToolHandlerNotImplemented = errors.New("ai tool handler not implemented")
)

type Registry struct {
	metadata map[string]domain.Metadata
	tools    map[string]einotool.InvokableTool
}

// 初始化工具注册表
func NewRegistry(timeout config.ToolTimeoutConfig) *Registry {
	queryTimeout := timeout.QuerySeconds
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeoutSeconds
	}
	writeTimeout := timeout.WriteSeconds
	if writeTimeout <= 0 {
		writeTimeout = defaultWriteTimeoutSeconds
	}

	registry := &Registry{
		metadata: make(map[string]domain.Metadata),
		tools:    make(map[string]einotool.InvokableTool),
	}
	for _, spec := range defaultToolSpecs(queryTimeout, writeTimeout) {
		registry.register(spec)
	}
	return registry
}

func (r *Registry) Metadata(name string) (domain.Metadata, error) {
	metadata, ok := r.metadata[name]
	if !ok {
		return domain.Metadata{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return metadata, nil
}

// 获取指定工具实例
func (r *Registry) Tool(name string) (einotool.InvokableTool, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return tool, nil
}

// 获取所有已注册的工具列表
func (r *Registry) Tools() []einotool.InvokableTool {
	names := r.sortedNames()
	result := make([]einotool.InvokableTool, 0, len(names))
	for _, name := range names {
		result = append(result, r.tools[name])
	}
	return result
}

// 获取所有工具的信息描述
func (r *Registry) ToolInfos(ctx context.Context) ([]*schema.ToolInfo, error) {
	tools := r.Tools()
	result := make([]*schema.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}

// 获取所有工具的元数据
func (r *Registry) AllMetadata() []domain.Metadata {
	names := r.sortedNames()
	result := make([]domain.Metadata, 0, len(names))
	for _, name := range names {
		result = append(result, r.metadata[name])
	}
	return result
}

// 注册工具到注册表
func (r *Registry) register(spec toolSpec) {
	metadata := domain.Metadata{
		Name:                spec.name,
		Risk:                spec.risk,
		RequireConfirmation: spec.requireConfirmation,
		TimeoutSeconds:      spec.timeoutSeconds,
		WriteOperation:      spec.writeOperation,
		RPCService:          spec.rpcService,
		RPCMethod:           spec.rpcMethod,
	}
	r.metadata[spec.name] = metadata
	r.tools[spec.name] = staticTool{
		info: &schema.ToolInfo{
			Name:        spec.name,
			Desc:        spec.desc,
			ParamsOneOf: schema.NewParamsOneOfByParams(spec.params),
		},
	}
}

// 获取按字母排序的工具名称列表
func (r *Registry) sortedNames() []string {
	names := make([]string, 0, len(r.metadata))
	for name := range r.metadata {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type staticTool struct {
	info *schema.ToolInfo
}

func (t staticTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t staticTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return "", ErrToolHandlerNotImplemented
}

type toolSpec struct {
	name                string
	desc                string
	risk                string
	requireConfirmation bool
	timeoutSeconds      int64
	writeOperation      bool
	rpcService          string
	rpcMethod           string
	params              map[string]*schema.ParameterInfo
}

// 获取默认的工具规格列表：
// 1. 搜索产品
// 2. 获取产品详情
// 3. 推荐产品
// 4. 获取库存
// 5. 获取订单
// 6. 列出订单
// 7. 结算订单
// 8. 获取结账详情
// 9. 列出购物车
// 10. 添加购物车
// 11. 减少购物车数量
// 12. 删除购物车项
// 13. 列出优惠券
// 14. 获取优惠券详情
// 15. 领取优惠券
// 16. 列出用户优惠券
// 17. 列出优惠券使用记录
// 18. 计算优惠券折扣
// 19. 创建订单
// 20. 取消订单
func defaultToolSpecs(queryTimeout, writeTimeout int64) []toolSpec {
	return []toolSpec{
		queryTool(domain.ToolProductSearch, "Search products by keyword, category, price range, or sort preference.", queryTimeout, "ProductCatalogService", "QueryProduct", params(
			stringParam("keyword", "Search keyword or natural language product need.", false),
			stringParam("category", "Product category name or ID.", false),
			numberParam("min_price", "Minimum acceptable price.", false),
			numberParam("max_price", "Maximum acceptable price.", false),
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of products to return.", false),
		)),
		queryTool(domain.ToolProductDetail, "Get product details by product ID.", queryTimeout, "ProductCatalogService", "GetProduct", params(
			integerParam("product_id", "Product ID.", true),
		)),
		queryTool(domain.ToolProductRecommend, "Recommend products for a user's stated shopping need.", queryTimeout, "ProductCatalogService", "RecommendProduct", params(
			stringParam("query", "Natural language recommendation need.", true),
			stringParam("category", "Preferred product category.", false),
			numberParam("min_price", "Minimum acceptable price.", false),
			numberParam("max_price", "Maximum acceptable price.", false),
			integerParam("limit", "Maximum number of recommendations.", false),
		)),
		queryTool(domain.ToolInventoryGet, "Get current inventory for a product.", queryTimeout, "Inventory", "GetInventory", params(
			integerParam("product_id", "Product ID.", true),
		)),
		queryTool(domain.ToolOrderGet, "Get one order by order ID.", queryTimeout, "OrderService", "GetOrder", params(
			stringParam("order_id", "Order ID.", true),
		)),
		queryTool(domain.ToolOrderList, "List current user's orders.", queryTimeout, "OrderService", "ListOrders", params(
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of orders to return.", false),
			stringParam("status", "Optional order status filter.", false),
		)),
		writeTool(domain.ToolCheckoutPrepare, "Prepare checkout for selected products.", domain.RiskLow, false, writeTimeout, "CheckoutService", "PrepareCheckout", params(
			objectArrayParam("order_items", "Products and quantities to reserve for checkout.", true, params(
				integerParam("product_id", "Product ID.", true),
				integerParam("quantity", "Purchase quantity.", true),
			)),
			stringParam("coupon_id", "Coupon ID to apply during checkout.", false),
		)),
		queryTool(domain.ToolCheckoutDetail, "Get checkout detail by pre-order ID.", queryTimeout, "CheckoutService", "GetCheckoutDetail", params(
			stringParam("pre_order_id", "Pre-order ID.", true),
		)),
		queryTool(domain.ToolCartList, "List current user's cart items.", queryTimeout, "Cart", "CartItemList", params(
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of cart items to return.", false),
		)),
		writeTool(domain.ToolCartAdd, "Add a product or SKU to current user's cart.", domain.RiskLow, false, writeTimeout, "Cart", "CreateCartItem", params(
			integerParam("product_id", "Product ID.", true),
			integerParam("sku_id", "SKU ID.", false),
			integerParam("quantity", "Quantity to add.", true),
		)),
		writeTool(domain.ToolCartSub, "Decrease quantity of an item in current user's cart.", domain.RiskLow, false, writeTimeout, "Cart", "SubCartItem", params(
			integerParam("cart_item_id", "Cart item ID.", true),
			integerParam("quantity", "Quantity to subtract.", true),
		)),
		writeTool(domain.ToolCartDelete, "Delete an item from current user's cart after confirmation.", domain.RiskHigh, true, writeTimeout, "Cart", "DeleteCartItem", params(
			integerParam("cart_item_id", "Cart item ID.", true),
		)),
		queryTool(domain.ToolCouponList, "List available coupons.", queryTimeout, "Coupons", "ListCoupons", params(
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of coupons to return.", false),
			integerParam("type", "Optional coupon type: 1 full reduction, 2 discount, 3 fixed amount.", false),
		)),
		queryTool(domain.ToolCouponDetail, "Get coupon detail by coupon ID.", queryTimeout, "Coupons", "GetCoupon", params(
			stringParam("coupon_id", "Coupon ID.", true),
		)),
		writeTool(domain.ToolCouponClaim, "Claim a coupon for current user.", domain.RiskLow, false, writeTimeout, "Coupons", "ClaimCoupon", params(
			stringParam("coupon_id", "Coupon ID.", true),
		)),
		queryTool(domain.ToolCouponMyList, "List coupons claimed by current user.", queryTimeout, "Coupons", "ListUserCoupons", params(
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of user coupons to return.", false),
			stringParam("status", "Optional user coupon status filter.", false),
		)),
		queryTool(domain.ToolCouponUsageList, "List current user's coupon usage records.", queryTimeout, "Coupons", "ListCouponUsages", params(
			integerParam("page", "Page number starting from 1.", false),
			integerParam("page_size", "Number of usage records to return.", false),
		)),
		queryTool(domain.ToolCouponCalculate, "Calculate coupon discount for checkout.", queryTimeout, "Coupons", "CalculateCoupon", params(
			stringParam("coupon_id", "Coupon ID.", true),
			objectArrayParam("items", "Products and quantities used to calculate the discount.", true, params(
				integerParam("product_id", "Product ID.", true),
				integerParam("quantity", "Product quantity.", true),
			)),
		)),
		writeTool(domain.ToolOrderCreate, "Create an order from a checkout after confirmation.", domain.RiskHigh, true, writeTimeout, "OrderService", "CreateOrder", params(
			stringParam("pre_order_id", "Pre-order ID.", true),
			stringParam("coupon_id", "Coupon ID to use for order creation.", false),
			integerParam("address_id", "Delivery address ID.", true),
			integerParam("payment_method", "Payment method: 1 WeChat Pay, 2 Alipay.", true),
		)),
		writeTool(domain.ToolOrderCancel, "Cancel an order after confirmation.", domain.RiskHigh, true, writeTimeout, "OrderService", "CancelOrder", params(
			stringParam("order_id", "Order ID.", true),
			stringParam("reason", "Cancellation reason.", false),
		)),
	}
}

func queryTool(name, desc string, timeout int64, service, method string, params map[string]*schema.ParameterInfo) toolSpec {
	return toolSpec{
		name:           name,
		desc:           desc,
		risk:           domain.RiskLow,
		timeoutSeconds: timeout,
		rpcService:     service,
		rpcMethod:      method,
		params:         params,
	}
}

func writeTool(name, desc, risk string, requireConfirmation bool, timeout int64, service, method string, params map[string]*schema.ParameterInfo) toolSpec {
	return toolSpec{
		name:                name,
		desc:                desc,
		risk:                risk,
		requireConfirmation: requireConfirmation,
		timeoutSeconds:      timeout,
		writeOperation:      true,
		rpcService:          service,
		rpcMethod:           method,
		params:              params,
	}
}

type namedParam struct {
	name string
	info *schema.ParameterInfo
}

// 构建参数映射表
func params(items ...namedParam) map[string]*schema.ParameterInfo {
	result := make(map[string]*schema.ParameterInfo, len(items))
	for _, item := range items {
		result[item.name] = item.info
	}
	return result
}

func stringParam(name, desc string, required bool) namedParam {
	return parameter(name, schema.String, desc, required)
}

func integerParam(name, desc string, required bool) namedParam {
	return parameter(name, schema.Integer, desc, required)
}

func numberParam(name, desc string, required bool) namedParam {
	return parameter(name, schema.Number, desc, required)
}

func objectArrayParam(name, desc string, required bool, fields map[string]*schema.ParameterInfo) namedParam {
	return namedParam{
		name: name,
		info: &schema.ParameterInfo{
			Type:     schema.Array,
			Desc:     desc,
			Required: required,
			ElemInfo: &schema.ParameterInfo{
				Type:      schema.Object,
				SubParams: fields,
			},
		},
	}
}

// 构建参数信息
func parameter(name string, dataType schema.DataType, desc string, required bool) namedParam {
	return namedParam{
		name: name,
		info: &schema.ParameterInfo{
			Type:     dataType,
			Desc:     desc,
			Required: required,
		},
	}
}

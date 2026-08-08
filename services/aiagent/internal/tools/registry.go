package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	tool_prompts "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/tools"
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
	tools    map[string]Tool
	executor *Executor
}

// 将工具注册到Registry表中
func NewRegistry(provided ...[]Tool) *Registry {
	registry := &Registry{
		metadata: make(map[string]domain.Metadata),
		tools:    make(map[string]Tool),
	}
	if len(provided) > 0 {
		for _, tool := range provided[0] {
			registry.registerTool(tool)
		}
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
	return &invokableToolAdapter{tool: tool, executor: r.executor}, nil
}

// 获取所有已注册的工具列表
func (r *Registry) Tools() []einotool.InvokableTool {
	names := r.sortedNames()
	result := make([]einotool.InvokableTool, 0, len(names))
	for _, name := range names {
		result = append(result, &invokableToolAdapter{tool: r.tools[name], executor: r.executor})
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

// ToolsByNames returns registered tools in the order requested by names.
func (r *Registry) ToolsByNames(names ...string) ([]einotool.InvokableTool, error) {
	result := make([]einotool.InvokableTool, 0, len(names))
	for _, name := range names {
		tool, err := r.Tool(name)
		if err != nil {
			return nil, err
		}
		result = append(result, tool)
	}
	return result, nil
}

// ToolInfosByNames returns tool schemas in the order requested by names.
func (r *Registry) ToolInfosByNames(ctx context.Context, names ...string) ([]*schema.ToolInfo, error) {
	tools, err := r.ToolsByNames(names...)
	if err != nil {
		return nil, err
	}
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

func (r *Registry) Handler(name string) (HandlerFunc, bool) {
	tool, ok := r.tools[name]
	if !ok || tool.Handler == nil {
		return nil, false
	}
	return tool.Handler, true
}

func (r *Registry) RequiresConfirmation(name string) bool {
	tool, ok := r.tools[name]
	if !ok {
		return false
	}
	return tool.Metadata.Risk == domain.RiskHigh &&
		tool.Metadata.RequireConfirmation &&
		tool.Metadata.WriteOperation &&
		tool.ConfirmationSummary != nil
}

func (r *Registry) ConfirmationSummary(ctx context.Context, req ExecuteRequest) (string, error) {
	tool, ok := r.tools[req.ToolName]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrToolNotFound, req.ToolName)
	}
	if tool.ConfirmationSummary == nil {
		return "", ErrToolHandlerRequired
	}
	return tool.ConfirmationSummary(ctx, req)
}

func (r *Registry) registerTool(tool Tool) {
	r.metadata[tool.Name] = tool.Metadata
	r.tools[tool.Name] = tool
}

func (r *Registry) setExecutor(executor *Executor) {
	r.executor = executor
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

// 注册tool工具信息列表
func defaultSchemaTools(queryTimeout, writeTimeout int64) []Tool {
	return []Tool{
		queryTool(domain.ToolProductSearch, tool_prompts.ProductSearchDesc, queryTimeout, "ProductService", "QueryProduct", tool_prompts.ProductSearchParameters),
		queryTool(domain.ToolProductDetail, tool_prompts.ProductDetailDesc, queryTimeout, "ProductCatalogService", "GetProduct", tool_prompts.ProductDetailParameters),
		queryTool(domain.ToolProductRecommend, tool_prompts.ProductRecommendDesc, queryTimeout, "ProductCatalogService", "RecommendProduct", tool_prompts.ProductRecommendParameters),
		queryTool(domain.ToolInventoryGet, tool_prompts.InventoryGetDesc, queryTimeout, "Inventory", "GetInventory", tool_prompts.InventoryGetParameters),
		queryTool(domain.ToolOrderGet, tool_prompts.OrderGetDesc, queryTimeout, "OrderService", "GetOrder", tool_prompts.OrderGetParameters),
		queryTool(domain.ToolOrderList, tool_prompts.OrderListDesc, queryTimeout, "OrderService", "ListOrders", tool_prompts.OrderListParameters),
		writeTool(domain.ToolCheckoutPrepare, tool_prompts.CheckoutPrepareDesc, domain.RiskLow, false, writeTimeout, "CheckoutService", "PrepareCheckout", tool_prompts.CheckoutPrepareParameters),
		queryTool(domain.ToolCheckoutDetail, tool_prompts.CheckoutDetailDesc, queryTimeout, "CheckoutService", "GetCheckoutDetail", tool_prompts.CheckoutDetailParameters),
		queryTool(domain.ToolCartList, tool_prompts.CartListDesc, queryTimeout, "Cart", "CartItemList", tool_prompts.CartListParameters),
		writeTool(domain.ToolCartAdd, tool_prompts.CartAddDesc, domain.RiskLow, false, writeTimeout, "Cart", "CreateCartItem", tool_prompts.CartAddParameters),
		writeTool(domain.ToolCartSub, tool_prompts.CartSubDesc, domain.RiskLow, false, writeTimeout, "Cart", "SubCartItem", tool_prompts.CartSubParameters),
		writeTool(domain.ToolCartDelete, tool_prompts.CartDeleteDesc, domain.RiskHigh, true, writeTimeout, "Cart", "DeleteCartItem", tool_prompts.CartDeleteParameters),
		queryTool(domain.ToolCouponList, tool_prompts.CouponListDesc, queryTimeout, "Coupons", "ListCoupons", tool_prompts.CouponListParameters),
		queryTool(domain.ToolCouponDetail, tool_prompts.CouponDetailDesc, queryTimeout, "Coupons", "GetCoupon", tool_prompts.CouponDetailParameters),
		writeTool(domain.ToolCouponClaim, tool_prompts.CouponClaimDesc, domain.RiskLow, false, writeTimeout, "Coupons", "ClaimCoupon", tool_prompts.CouponClaimParameters),
		queryTool(domain.ToolCouponMyList, tool_prompts.CouponMyListDesc, queryTimeout, "Coupons", "ListUserCoupons", tool_prompts.CouponMyListParameters),
		queryTool(domain.ToolCouponUsageList, tool_prompts.CouponUsageListDesc, queryTimeout, "Coupons", "ListCouponUsages", tool_prompts.CouponUsageListParameters),
		queryTool(domain.ToolCouponCalculate, tool_prompts.CouponCalculateDesc, queryTimeout, "Coupons", "CalculateCoupon", tool_prompts.CouponCalculateParameters),
		writeTool(domain.ToolOrderCreate, tool_prompts.OrderCreateDesc, domain.RiskHigh, true, writeTimeout, "OrderService", "CreateOrder", tool_prompts.OrderCreateParameters),
		writeTool(domain.ToolOrderCancel, tool_prompts.OrderCancelDesc, domain.RiskHigh, true, writeTimeout, "OrderService", "CancelOrder", tool_prompts.OrderCancelParameters),
	}
}

func queryTool(name, desc string, timeout int64, service, method string, params map[string]*schema.ParameterInfo) Tool {
	return Tool{
		Name:   name,
		Desc:   desc,
		Params: params,
		Metadata: domain.Metadata{
			Name:           name,
			Risk:           domain.RiskLow,
			TimeoutSeconds: timeout,
			RPCService:     service,
			RPCMethod:      method,
		},
	}
}

func writeTool(name, desc, risk string, requireConfirmation bool, timeout int64, service, method string, params map[string]*schema.ParameterInfo) Tool {
	return Tool{
		Name:   name,
		Desc:   desc,
		Params: params,
		Metadata: domain.Metadata{
			Name:                name,
			Risk:                risk,
			RequireConfirmation: requireConfirmation,
			TimeoutSeconds:      timeout,
			WriteOperation:      true,
			RPCService:          service,
			RPCMethod:           method,
		},
	}
}

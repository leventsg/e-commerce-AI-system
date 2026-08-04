package tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"google.golang.org/grpc"
)

type ProductQueryRPC interface {
	QueryProduct(ctx context.Context, in *productcatalogservice.QueryProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error)
	GetProduct(ctx context.Context, in *productcatalogservice.GetProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetProductResp, error)
	RecommendProduct(ctx context.Context, in *productcatalogservice.RecommendProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error)
}

// 商品查询：查询、详情、推荐商品
func productQueryHandlers(rpc ProductQueryRPC) map[string]HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]HandlerFunc{
		domain.ToolProductSearch:    productSearchHandler(rpc),
		domain.ToolProductDetail:    productDetailHandler(rpc),
		domain.ToolProductRecommend: productRecommendHandler(rpc),
	}
}

func productSearchHandler(rpc ProductQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 解析查询参数
		keyword, err := optionalStringArgument(req.Arguments, "keyword")
		if err != nil {
			return HandlerResult{}, err
		}
		categories, err := optionalStringListArgument(req.Arguments, "category")
		if err != nil {
			return HandlerResult{}, err
		}
		minPrice, err := optionalInt64Argument(req.Arguments, "min_price", 0)
		if err != nil {
			return HandlerResult{}, err
		}
		maxPrice, err := optionalInt64Argument(req.Arguments, "max_price", 0)
		if err != nil {
			return HandlerResult{}, err
		}
		if minPrice < 0 || maxPrice < 0 || (maxPrice > 0 && minPrice > maxPrice) {
			return HandlerResult{}, invalidArgument("price", "range is invalid")
		}
		page, pageSize, err := queryPagination(req.Arguments)
		if err != nil {
			return HandlerResult{}, err
		}

		// 构造请求参数
		rpcReq := &productcatalogservice.QueryProductReq{
			Keyword:  keyword,
			Category: categories,
			Paginator: &productcatalogservice.QueryProductReq_Paginator{
				Page:     int64(page),
				PageSize: int64(pageSize),
			},
		}
		if minPrice > 0 || maxPrice > 0 {
			rpcReq.Price = &productcatalogservice.QueryProductReq_Price{Min: minPrice, Max: maxPrice}
		}

		// 调用 RPC 查询商品
		resp, err := rpc.QueryProduct(ctx, rpcReq)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("product_search rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: product_search returned nil response", ErrQueryRPCUnavailable)
		}
		if err := validateRPCResponse("product_search", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		// 将查询结果转为map格式
		products := compactProducts(resp.Products, false)
		return HandlerResult{
			Data: map[string]any{
				"total":     resp.Total,
				"page":      page,
				"page_size": pageSize,
				"products":  products,
			},
			Summary: fmt.Sprintf("找到 %d 件商品。", len(products)),
		}, nil
	}
}

func productDetailHandler(rpc ProductQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 解析查询参数
		productIDValue, err := requiredInt64Argument(req.Arguments, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		productID, err := positiveUint32(productIDValue, "product_id")
		if err != nil {
			return HandlerResult{}, err
		}
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}

		// 调用 RPC 查询商品详情
		resp, err := rpc.GetProduct(ctx, &productcatalogservice.GetProductReq{Id: productID, UserId: userID})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("product_detail rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: product_detail returned nil response", ErrQueryRPCUnavailable)
		}
		// 验证 RPC 响应
		if err := validateRPCResponse("product_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		if resp.Product == nil {
			return HandlerResult{}, fmt.Errorf("%w: product_detail returned empty product", ErrQueryRPCUnavailable)
		}
		return HandlerResult{
			Data:    map[string]any{"product": compactProduct(resp.Product, true)},
			Summary: fmt.Sprintf("已查询商品“%s”的详情。", resp.Product.Name),
		}, nil
	}
}

func productRecommendHandler(rpc ProductQueryRPC) HandlerFunc {
	return func(ctx context.Context, req HandlerRequest) (HandlerResult, error) {
		// 检查参数中是否包含“query”字符串
		if _, err := requiredStringArgument(req.Arguments, "query"); err != nil {
			return HandlerResult{}, err
		}
		// 解析查询参数
		categories, err := optionalStringListArgument(req.Arguments, "category")
		if err != nil {
			return HandlerResult{}, err
		}
		limitValue, err := optionalInt64Argument(req.Arguments, "limit", 5)
		if err != nil {
			return HandlerResult{}, err
		}
		limit, err := positiveInt32(limitValue, "limit")
		if err != nil {
			return HandlerResult{}, err
		}
		if limit > 20 {
			limit = 20
		}
		minPrice, err := optionalInt64Argument(req.Arguments, "min_price", 0)
		if err != nil {
			return HandlerResult{}, err
		}
		maxPrice, err := optionalInt64Argument(req.Arguments, "max_price", 0)
		if err != nil {
			return HandlerResult{}, err
		}
		if minPrice < 0 || maxPrice < 0 || (maxPrice > 0 && minPrice > maxPrice) {
			return HandlerResult{}, invalidArgument("price", "range is invalid")
		}
		userID, err := authenticatedUserID32(req.UserID)
		if err != nil {
			return HandlerResult{}, err
		}
		// 调用 RPC 查询商品推荐
		resp, err := rpc.RecommendProduct(ctx, &productcatalogservice.RecommendProductReq{
			UserId:   userID,
			Category: categories,
			Paginator: &productcatalogservice.RecommendProductReq_Paginator{
				Page: 1, PageSize: int64(limit),
			},
		})
		if err != nil {
			return HandlerResult{}, fmt.Errorf("product_recommend rpc: %w", err)
		}
		if resp == nil {
			return HandlerResult{}, fmt.Errorf("%w: product_recommend returned nil response", ErrQueryRPCUnavailable)
		}
		// 验证 RPC 响应
		if err := validateRPCResponse("product_recommend", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return HandlerResult{}, err
		}
		// 过滤商品列表，确保价格在指定范围内，并限制返回数量
		filtered := make([]*productcatalogservice.Product, 0, len(resp.Products))
		for _, product := range resp.Products {
			if product == nil || (minPrice > 0 && product.Price < minPrice) || (maxPrice > 0 && product.Price > maxPrice) {
				continue
			}
			filtered = append(filtered, product)
			if len(filtered) == int(limit) {
				break
			}
		}
		// 将查询结果转为map格式
		products := compactProducts(filtered, false)
		return HandlerResult{
			Data:    map[string]any{"total": len(products), "products": products},
			Summary: fmt.Sprintf("为你推荐了 %d 件商品。", len(products)),
		}, nil
	}
}

// 将商品列表转换为map格式
func compactProducts(products []*productcatalogservice.Product, detail bool) []map[string]any {
	result := make([]map[string]any, 0, len(products))
	for _, product := range products {
		if product != nil {
			result = append(result, compactProduct(product, detail))
		}
	}
	return result
}

func compactProduct(product *productcatalogservice.Product, detail bool) map[string]any {
	result := map[string]any{
		"product_id": product.Id,
		"name":       product.Name,
		"price":      product.Price,
		"stock":      product.Stock,
		"sold":       product.Sold,
		"picture":    product.Picture,
		"categories": append([]string(nil), product.Categories...),
	}
	if detail {
		result["description"] = product.Description
	}
	return result
}

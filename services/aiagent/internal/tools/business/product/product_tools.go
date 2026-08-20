package product_tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"google.golang.org/grpc"
)

type ProductQueryRPC interface {
	QueryProduct(ctx context.Context, in *productcatalogservice.QueryProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error)
	GetProduct(ctx context.Context, in *productcatalogservice.GetProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetProductResp, error)
	RecommendProduct(ctx context.Context, in *productcatalogservice.RecommendProductReq, opts ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error)
}

// 商品查询：查询、详情、推荐商品
func ProductQueryHandlers(rpc ProductQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolProductSearch:    productSearchHandler(rpc),
		domain.ToolProductDetail:    productDetailHandler(rpc),
		domain.ToolProductRecommend: productRecommendHandler(rpc),
	}
}

func productSearchHandler(rpc ProductQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		// 解析查询参数
		keyword, err := helper.OptionalStringArgument(req.Arguments, "keyword")
		if err != nil {
			return core.HandlerResult{}, err
		}
		categories, err := helper.OptionalStringListArgument(req.Arguments, "category")
		if err != nil {
			return core.HandlerResult{}, err
		}
		var minPrice, maxPrice int64
		if price, ok := req.Arguments["price"].(map[string]any); ok {
			minPrice, err = helper.OptionalInt64Argument(price, "min", 0)
			if err != nil {
				return core.HandlerResult{}, err
			}
			maxPrice, err = helper.OptionalInt64Argument(price, "max", 0)
			if err != nil {
				return core.HandlerResult{}, err
			}
		} else {
			minPrice, err = helper.OptionalInt64Argument(req.Arguments, "min_price", 0)
			if err != nil {
				return core.HandlerResult{}, err
			}
			maxPrice, err = helper.OptionalInt64Argument(req.Arguments, "max_price", 0)
			if err != nil {
				return core.HandlerResult{}, err
			}
		}
		if minPrice < 0 || maxPrice < 0 || (maxPrice > 0 && minPrice > maxPrice) {
			return core.HandlerResult{}, helper.InvalidArgument("price", "range is invalid")
		}
		page, pageSize, err := helper.QueryPagination(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
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
			return core.HandlerResult{}, fmt.Errorf("product_search rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: product_search returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("product_search", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		// 将查询结果转为map格式
		products := compactProducts(resp.Products, false)
		return core.HandlerResult{
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

func productDetailHandler(rpc ProductQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		// 解析查询参数
		productIDValue, err := helper.RequiredInt64Argument(req.Arguments, "product_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		productID, err := helper.PositiveUint32(productIDValue, "product_id")
		if err != nil {
			return core.HandlerResult{}, err
		}
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}

		// 调用 RPC 查询商品详情
		resp, err := rpc.GetProduct(ctx, &productcatalogservice.GetProductReq{Id: productID, UserId: userID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("product_detail rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: product_detail returned nil response", helper.ErrQueryRPCUnavailable)
		}
		// 验证 RPC 响应
		if err := helper.ValidateRPCResponse("product_detail", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Product == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: product_detail returned empty product", helper.ErrQueryRPCUnavailable)
		}
		return core.HandlerResult{
			Data:    map[string]any{"product": compactProduct(resp.Product, true)},
			Summary: fmt.Sprintf("已查询商品“%s”的详情。", resp.Product.Name),
		}, nil
	}
}

func productRecommendHandler(rpc ProductQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		// 检查参数中是否包含“query”字符串
		if _, err := helper.RequiredStringArgument(req.Arguments, "query"); err != nil {
			return core.HandlerResult{}, err
		}
		// 解析查询参数
		categories, err := helper.OptionalStringListArgument(req.Arguments, "category")
		if err != nil {
			return core.HandlerResult{}, err
		}
		limitValue, err := helper.OptionalInt64Argument(req.Arguments, "limit", 5)
		if err != nil {
			return core.HandlerResult{}, err
		}
		limit, err := helper.PositiveInt32(limitValue, "limit")
		if err != nil {
			return core.HandlerResult{}, err
		}
		if limit > 20 {
			limit = 20
		}
		minPrice, err := helper.OptionalInt64Argument(req.Arguments, "min_price", 0)
		if err != nil {
			return core.HandlerResult{}, err
		}
		maxPrice, err := helper.OptionalInt64Argument(req.Arguments, "max_price", 0)
		if err != nil {
			return core.HandlerResult{}, err
		}
		if minPrice < 0 || maxPrice < 0 || (maxPrice > 0 && minPrice > maxPrice) {
			return core.HandlerResult{}, helper.InvalidArgument("price", "range is invalid")
		}
		userID, err := helper.AuthenticatedUserID32(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
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
			return core.HandlerResult{}, fmt.Errorf("product_recommend rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: product_recommend returned nil response", helper.ErrQueryRPCUnavailable)
		}
		// 验证 RPC 响应
		if err := helper.ValidateRPCResponse("product_recommend", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
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
		return core.HandlerResult{
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

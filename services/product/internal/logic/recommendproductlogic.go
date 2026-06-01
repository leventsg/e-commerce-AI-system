package logic

import (
	"context"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/services/product/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/product/product"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

type RecommendProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRecommendProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RecommendProductLogic {
	return &RecommendProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RecommendProductLogic) RecommendProduct(in *product.RecommendProductReq) (*product.GetAllProductsResp, error) {
	// 1. 准备分页参数
	offset := (in.Paginator.Page - 1) * in.Paginator.PageSize
	n := in.Paginator.PageSize
	// 2. 调用Gorse推荐接口
	itemIds, err := l.svcCtx.GorseClient.GetItemRecommend(
		l.ctx,
		strconv.FormatInt(int64(in.UserId), 10),
		in.Category, // categories（根据业务需求可传分类列表）
		"read",      // 写回类型（根据实际场景调整）
		"5m",        // 写回延迟（根据实际场景调整）
		int(n),
		int(offset),
	)
	if err != nil {
		l.Logger.Errorw("gorse recommend failed", logx.Field("err", err))
		return nil, err
	}
	// 3. 根据推荐结果查询商品详情
	// 如果没有推荐结果，返回默认的热销商品列表
	if len(itemIds) == 0 {
		products, err := l.svcCtx.RedisClient.Zrevrange(biz.ProductRedisPVName, 0, 99)
		if err != nil {
			l.Logger.Errorw("Failed to get top 100 hot products",
				logx.Field("err", err),
			)
			return nil, err
		}
		for _, productID := range products {
			productId := strings.TrimPrefix(productID, biz.ProductIDKeyPrefix)
			itemIds = append(itemIds, productId)
		}
	}
	products, err := l.svcCtx.ProductModel.GetProductByIDs(l.ctx, itemIds)
	if err != nil {
		l.Logger.Errorw("query products failed", logx.Field("err", err))
		return nil, err
	}
	result := populateProductDetails(l.ctx, l.svcCtx, products)

	return &product.GetAllProductsResp{
		Products: result,
	}, nil
}

package product

import (
	"context"
	"strconv"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	product2 "github.com/leventsg/e-commerce-AI-system/dal/model/products/product"
	pc "github.com/leventsg/e-commerce-AI-system/dal/model/products/product_categories"
	"github.com/olivere/elastic/v7"
	"github.com/zeromicro/go-zero/core/logx"
)

// SyncProductsToES 启动时将 MySQL 中的商品按统一文档结构同步到 ES。
func SyncProductsToES(ctx context.Context, esClient *elastic.Client, productModel product2.ProductsModel, categoryModel pc.ProductCategoriesModel) {
	if esClient == nil || productModel == nil || categoryModel == nil {
		return
	}
	products, err := productModel.QueryAllProducts(ctx)
	if err != nil {
		logx.Errorw("sync products to es: query products failed", logx.Field("err", err))
		return
	}
	for _, product := range products {
		if product == nil {
			continue
		}
		categories, err := categoryModel.FindCategoriesByIds(ctx, product.Id)
		if err != nil {
			logx.Errorw("sync products to es: query categories failed", logx.Field("err", err), logx.Field("product_id", product.Id))
			continue
		}
		doc := BuildESProductDocument(product, categories)
		if _, err := esClient.Index().
			Index(biz.ProductEsIndexName).
			Id(strconv.FormatInt(product.Id, 10)).
			BodyJson(doc).
			Refresh("false").
			Do(ctx); err != nil {
			logx.Errorw("sync products to es: index failed", logx.Field("err", err), logx.Field("product_id", product.Id))
		}
	}
	logx.Infow("同步商品到ES完成", logx.Field("同步商品数量", len(products)))
}

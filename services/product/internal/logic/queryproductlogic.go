package logic

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	esproduct "github.com/leventsg/e-commerce-AI-system/dal/es/product"
	productmodel "github.com/leventsg/e-commerce-AI-system/dal/model/products/product"
	"github.com/leventsg/e-commerce-AI-system/services/product/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/services/product/product"
	"github.com/olivere/elastic/v7"
	"github.com/zeromicro/go-zero/core/logx"
)

type QueryProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewQueryProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryProductLogic {
	return &QueryProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// QueryProduct searches products with Elasticsearch, then enriches matched
// products from MySQL and inventory RPC so stock and sold stay realtime.
func (l *QueryProductLogic) QueryProduct(in *product.QueryProductReq) (*product.GetAllProductsResp, error) {
	boolQuery := l.buildESQuery(in)

	pageSize := biz.DefaultPageSize
	page := 1
	if in.Paginator != nil && in.Paginator.PageSize > 0 {
		pageSize = int(in.Paginator.PageSize)
	}
	if in.Paginator != nil && in.Paginator.Page > 0 {
		page = int(in.Paginator.Page)
	}
	from := (page - 1) * pageSize

	searchService := l.svcCtx.EsClient.Search().
		Index(biz.ProductEsIndexName).
		Query(boolQuery).
		From(from).
		Size(pageSize)

	if in.New {
		searchService.SortBy(
			elastic.NewFieldSort("created_at").Desc(),
			elastic.NewFieldSort("updated_at").Desc(),
		)
	}
	if in.Hot {
		searchService.SortBy(
			elastic.NewFieldSort("price").Asc(),
			elastic.NewScoreSort().Desc(),
		)
	}

	searchResult, err := searchService.Do(l.ctx)
	if err != nil {
		l.Logger.Errorw("elasticsearch query error", logx.Field("err", err))
		return nil, err
	}

	productIDs := make([]string, 0, len(searchResult.Hits.Hits))
	for _, hit := range searchResult.Hits.Hits {
		var doc esproduct.ESProductDocument
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			l.Logger.Errorw("unmarshal elasticsearch product document failed", logx.Field("err", err))
			continue
		}
		productIDs = append(productIDs, strconv.FormatUint(uint64(doc.ID), 10))
	}
	if len(productIDs) == 0 {
		return &product.GetAllProductsResp{
			Total:    searchResult.TotalHits(),
			Products: []*product.Product{},
		}, nil
	}

	productRows, err := productmodel.NewProductsModel(l.svcCtx.Mysql).GetProductByIDs(l.ctx, productIDs)
	if err != nil {
		l.Logger.Errorw("query products by elasticsearch result ids failed",
			logx.Field("err", err),
			logx.Field("product_ids", productIDs))
		return nil, err
	}

	productByID := make(map[int64]*productmodel.Products, len(productRows))
	for _, row := range productRows {
		if row != nil {
			productByID[row.Id] = row
		}
	}

	orderedProducts := make([]*productmodel.Products, 0, len(productIDs))
	for _, id := range productIDs {
		productID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			continue
		}
		row, ok := productByID[productID]
		if !ok {
			l.Logger.Infow("product from elasticsearch not found in mysql", logx.Field("product_id", productID))
			continue
		}
		orderedProducts = append(orderedProducts, row)
	}

	return &product.GetAllProductsResp{
		Total:    searchResult.TotalHits(),
		Products: populateProductDetails(l.ctx, l.svcCtx, orderedProducts),
	}, nil
}

func (l *QueryProductLogic) buildESQuery(req *product.QueryProductReq) *elastic.BoolQuery {
	boolQuery := elastic.NewBoolQuery()

	if req.Name != "" {
		boolQuery.Must(elastic.NewMatchQuery("name", req.Name))
	}
	if req.Keyword != "" {
		boolQuery.Should(
			elastic.NewMultiMatchQuery(req.Keyword, "name^1", "description^2").Operator("and"),
			elastic.NewMatchPhraseQuery("name", req.Keyword).Boost(1),
			elastic.NewMatchPhraseQuery("description", req.Keyword).Boost(3),
			elastic.NewTermQuery("description.keyword", req.Keyword).Boost(5),
		)
		boolQuery.MinimumNumberShouldMatch(1)
	}

	if len(req.Category) > 0 {
		boolQuery.Filter(elastic.NewTermsQuery("category", stringSliceToInterface(req.Category)...))
	}

	if req.Price != nil {
		rangeQuery := elastic.NewRangeQuery("price")
		if req.Price.Min > 0 {
			rangeQuery.Gte(req.Price.Min)
		}
		if req.Price.Max > 0 {
			rangeQuery.Lte(req.Price.Max)
		}
		boolQuery.Filter(rangeQuery)
	}

	return boolQuery
}

func stringSliceToInterface(s []string) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

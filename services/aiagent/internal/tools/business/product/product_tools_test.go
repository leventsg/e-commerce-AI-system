package product_tools

import (
	"context"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	"github.com/leventsg/e-commerce-AI-system/services/product/productcatalogservice"
	"google.golang.org/grpc"
)

func TestProductSearchHandlerParsesNestedPrice(t *testing.T) {
	fake := &fakeProductQueryRPC{}
	handlers := ProductQueryHandlers(fake)
	handler := handlers[domain.ToolProductSearch]
	if handler == nil {
		t.Fatal("product_search handler missing")
	}
	_, err := handler(context.Background(), core.HandlerRequest{
		Arguments: map[string]any{
			"keyword": "耳机",
			"price": map[string]any{
				"max": int64(40000),
			},
		},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if fake.queryReq == nil || fake.queryReq.Price == nil {
		t.Fatalf("price not forwarded: %+v", fake.queryReq)
	}
	if fake.queryReq.Price.Max != 40000 {
		t.Fatalf("max price=%d, want 40000", fake.queryReq.Price.Max)
	}
}

type fakeProductQueryRPC struct {
	queryReq *productcatalogservice.QueryProductReq
}

func (f *fakeProductQueryRPC) QueryProduct(_ context.Context, in *productcatalogservice.QueryProductReq, _ ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error) {
	f.queryReq = in
	return &productcatalogservice.GetAllProductsResp{}, nil
}

func (f *fakeProductQueryRPC) GetProduct(context.Context, *productcatalogservice.GetProductReq, ...grpc.CallOption) (*productcatalogservice.GetProductResp, error) {
	return &productcatalogservice.GetProductResp{}, nil
}

func (f *fakeProductQueryRPC) RecommendProduct(context.Context, *productcatalogservice.RecommendProductReq, ...grpc.CallOption) (*productcatalogservice.GetAllProductsResp, error) {
	return &productcatalogservice.GetAllProductsResp{}, nil
}

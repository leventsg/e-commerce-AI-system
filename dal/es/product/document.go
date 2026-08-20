package product

import (
	"database/sql"
	"time"

	product2 "github.com/leventsg/e-commerce-AI-system/dal/model/products/product"
)

// ESProductDocument 是写入 Elasticsearch 的统一文档结构，
// 字段名与 es mapping 和查询逻辑保持一致。
type ESProductDocument struct {
	ID          uint32   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Picture     string   `json:"picture"`
	Price       int64    `json:"price"`
	Categories  []string `json:"category,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// BuildESProductDocument 将数据库商品模型转换为统一 ES 文档。
func BuildESProductDocument(product *product2.Products, categories []string) ESProductDocument {
	if product == nil {
		return ESProductDocument{}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	createdAt := now
	updatedAt := now
	if !product.CreatedAt.IsZero() {
		createdAt = product.CreatedAt.Format("2006-01-02 15:04:05")
	}
	if !product.UpdatedAt.IsZero() {
		updatedAt = product.UpdatedAt.Format("2006-01-02 15:04:05")
	}
	return ESProductDocument{
		ID:          uint32(product.Id),
		Name:        product.Name,
		Description: nullStringValue(product.Description),
		Picture:     nullStringValue(product.Picture),
		Price:       product.Price,
		Categories:  append([]string(nil), categories...),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

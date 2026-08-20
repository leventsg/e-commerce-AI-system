package product

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	product2 "github.com/leventsg/e-commerce-AI-system/dal/model/products/product"
)

func TestBuildESProductDocumentUsesLowercaseFields(t *testing.T) {
	doc := BuildESProductDocument(&product2.Products{
		Id:          7,
		Name:        "we1",
		Description: sql.NullString{String: "dsd", Valid: true},
		Picture:     sql.NullString{String: "pic", Valid: true},
		Price:       21,
	}, []string{"1", "2"})

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"id":7`, `"name":"we1"`, `"description":"dsd"`, `"price":21`, `"category":["1","2"]`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("document %s missing %s", raw, key)
		}
	}
}

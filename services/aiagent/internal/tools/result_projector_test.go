package tools

import (
	"errors"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestResultProjectorExtractsAllowedEntityIDsAndState(t *testing.T) {
	createdAt := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	dataJSON := `{
		"items": [
			{"cart_item_id": 12, "product_id": 88, "quantity": 2, "name": "very long product text"},
			{"cart_item_id": "13", "product_id": "89", "quantity": 1}
		],
		"status": "active",
		"debug_payload": {"secret": "ignored"}
	}`

	ref, err := ProjectToolCallRef(domain.ToolCartList, "success", "购物车共有 2 件商品", "call-1", createdAt, []byte(dataJSON))
	if err != nil {
		t.Fatalf("ProjectToolCallRef() error = %v", err)
	}

	if ref.ToolCallID != "call-1" || ref.ToolName != domain.ToolCartList || ref.Status != "success" || ref.Summary != "购物车共有 2 件商品" || !ref.CreatedAt.Equal(createdAt) {
		t.Fatalf("ref basics = %+v", ref)
	}
	assertStringSet(t, ref.EntityIDs["cart_item_ids"], []string{"12", "13"})
	assertStringSet(t, ref.EntityIDs["product_ids"], []string{"88", "89"})
	if _, ok := ref.EntityIDs["debug_payload"]; ok {
		t.Fatalf("unexpected debug payload in entity ids: %+v", ref.EntityIDs)
	}
	if ref.State["status"] != "active" {
		t.Fatalf("status state = %#v", ref.State["status"])
	}
	assertStringSet(t, valuesAsStrings(ref.State["quantities"]), []string{"2", "1"})
}

func TestResultProjectorPreservesOrderIDs(t *testing.T) {
	ref, err := ProjectToolCallRef(domain.ToolOrderGet, "success", "订单已查询", "call-order", time.Time{}, []byte(`{
		"order_id": "202607230000000001",
		"status": "pending_payment",
		"quantity": 3
	}`))
	if err != nil {
		t.Fatalf("ProjectToolCallRef() error = %v", err)
	}

	assertStringSet(t, ref.EntityIDs["order_ids"], []string{"202607230000000001"})
	if ref.State["status"] != "pending_payment" || valuesAsStrings(ref.State["quantities"])[0] != "3" {
		t.Fatalf("state = %+v", ref.State)
	}
}

func TestResultProjectorDoesNotNormalizeInternalMetadata(t *testing.T) {
	ref, err := ProjectToolCallRef(domain.ToolCartList, " success ", " summary ", " call-1 ", time.Time{}, []byte(`{"items":[{"cart_item_id":1}]}`))
	if err != nil {
		t.Fatalf("ProjectToolCallRef() error = %v", err)
	}
	if ref.ToolCallID != " call-1 " || ref.Status != " success " || ref.Summary != " summary " {
		t.Fatalf("ref metadata was normalized unexpectedly: %+v", ref)
	}
}

func TestResultProjectorDoesNotTrimEntityValues(t *testing.T) {
	ref, err := ProjectToolCallRef(domain.ToolOrderGet, "success", "订单已查询", "call-order", time.Time{}, []byte(`{
		"order_id": " 202607230000000001 "
	}`))
	if err != nil {
		t.Fatalf("ProjectToolCallRef() error = %v", err)
	}
	assertStringSet(t, ref.EntityIDs["order_ids"], []string{" 202607230000000001 "})
}

func TestResultProjectorRejectsUnknownToolAndInvalidJSON(t *testing.T) {
	if _, err := ProjectToolCallRef("unknown.tool", "success", "", "call-1", time.Time{}, []byte(`{}`)); !errors.Is(err, ErrUnsupportedToolProjection) {
		t.Fatalf("unknown tool error = %v, want ErrUnsupportedToolProjection", err)
	}
	if _, err := ProjectToolCallRef(domain.ToolCartList, "success", "", "call-1", time.Time{}, []byte(`not-json`)); !errors.Is(err, ErrInvalidToolResultJSON) {
		t.Fatalf("invalid json error = %v, want ErrInvalidToolResultJSON", err)
	}
}

func assertStringSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(%v) = %d, want %d (%v)", got, len(got), len(want), want)
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			t.Fatalf("values = %v, missing %q", got, value)
		}
	}
}

func valuesAsStrings(value any) []string {
	values, _ := value.([]string)
	return values
}

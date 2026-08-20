package tool_prompts

import "testing"

func TestOrderCreateParametersFixedAlipay(t *testing.T) {
	if _, ok := OrderCreateParameters["payment_method"]; ok {
		t.Fatal("order_create schema must not require payment_method")
	}
	if _, ok := OrderCreateParameters["pre_order_id"]; !ok {
		t.Fatal("order_create schema missing pre_order_id")
	}
	if _, ok := OrderCreateParameters["address_id"]; !ok {
		t.Fatal("order_create schema missing address_id")
	}
}

//go:build ai_business_agents

package eino

import (
	"context"
	"slices"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// A missing whitelist entry drops a capability; an extra write entry grants
// another agent authority to perform the same operation.
func TestBusinessAgentToolPermissions(t *testing.T) {
	expected := map[string][]string{
		"product_discovery_agent": {"product_search", "product_recommend", "product_detail", "inventory_get"},
		"shopping_decision_agent": {"product_search", "product_recommend", "product_detail", "inventory_get", "coupon_list", "coupon_detail", "coupon_claim", "coupon_my_list", "coupon_usage_list", "coupon_calculate", "cart_list", "cart_add", "cart_sub", "cart_delete"},
		"order_fulfillment_agent": {"address_list", "address_get", "address_add", "address_update", "address_delete", "cart_list", "checkout_prepare", "checkout_detail", "order_create", "order_get", "order_list"},
		"after_sales_agent":       {"order_get", "order_list", "order_cancel", "coupon_usage_list", "coupon_detail", "product_detail"},
		"general_agent":           {"user_get", "user_update"},
	}
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	if len(supervisorSubAgentSpecs) != len(expected) {
		t.Fatalf("registered %d subagents, want %d", len(supervisorSubAgentSpecs), len(expected))
	}
	owners := map[string]string{}
	for _, spec := range supervisorSubAgentSpecs {
		want, ok := expected[spec.name]
		if !ok {
			t.Fatalf("unexpected agent %s", spec.name)
		}
		infos, err := registry.SubAgentToolInfos(context.Background(), spec.tools...)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, info := range infos {
			got = append(got, info.Name)
			metadata, err := registry.Metadata(info.Name)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.WriteOperation {
				if owner := owners[info.Name]; owner != "" {
					t.Fatalf("write tool %s shared by %s and %s", info.Name, owner, spec.name)
				}
				owners[info.Name] = spec.name
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s tools = %v, want %v", spec.name, got, want)
		}
		delete(expected, spec.name)
	}
	for _, metadata := range registry.AllMetadata() {
		if metadata.WriteOperation && owners[metadata.Name] == "" {
			t.Errorf("unowned write tool %s", metadata.Name)
		}
	}
	for _, name := range []string{"cart_delete", "order_create", "order_cancel"} {
		metadata, err := registry.Metadata(name)
		if err != nil {
			t.Fatal(err)
		}
		if !metadata.RequireConfirmation || metadata.Risk != "high" {
			t.Errorf("%s lost confirmation protection", name)
		}
	}
}

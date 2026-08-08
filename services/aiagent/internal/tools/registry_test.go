package tools

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

var openAIToolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func TestRegistryRegistersDefaultTools(t *testing.T) {
	registry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})

	metadata := registry.AllMetadata()
	if len(metadata) != 20 {
		t.Fatalf("len(AllMetadata) = %d, want 20", len(metadata))
	}

	seen := make(map[string]bool, len(metadata))
	for _, item := range metadata {
		if item.Name == "" {
			t.Fatal("metadata name should not be empty")
		}
		if seen[item.Name] {
			t.Fatalf("duplicate tool metadata for %q", item.Name)
		}
		seen[item.Name] = true

		tool, err := registry.Tool(item.Name)
		if err != nil {
			t.Fatalf("Tool(%q) returned error: %v", item.Name, err)
		}
		if tool == nil {
			t.Fatalf("Tool(%q) returned nil", item.Name)
		}
	}

	for _, name := range expectedToolNames() {
		if !seen[name] {
			t.Fatalf("tool %q was not registered", name)
		}
	}
}

func TestRegistryRiskAndConfirmationMetadata(t *testing.T) {
	registry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})

	for _, name := range []string{domain.ToolCartDelete, domain.ToolOrderCreate, domain.ToolOrderCancel} {
		metadata, err := registry.Metadata(name)
		if err != nil {
			t.Fatalf("Metadata(%q) returned error: %v", name, err)
		}
		if metadata.Risk != domain.RiskHigh {
			t.Fatalf("%s Risk = %q, want %q", name, metadata.Risk, domain.RiskHigh)
		}
		if !metadata.RequireConfirmation {
			t.Fatalf("%s RequireConfirmation = false, want true", name)
		}
		if !metadata.WriteOperation {
			t.Fatalf("%s WriteOperation = false, want true", name)
		}
	}

	for _, name := range []string{domain.ToolProductSearch, domain.ToolOrderGet, domain.ToolCartList, domain.ToolCouponCalculate} {
		metadata, err := registry.Metadata(name)
		if err != nil {
			t.Fatalf("Metadata(%q) returned error: %v", name, err)
		}
		if metadata.Risk != domain.RiskLow {
			t.Fatalf("%s Risk = %q, want %q", name, metadata.Risk, domain.RiskLow)
		}
		if metadata.RequireConfirmation {
			t.Fatalf("%s RequireConfirmation = true, want false", name)
		}
		if metadata.WriteOperation {
			t.Fatalf("%s WriteOperation = true, want false", name)
		}
	}

	for _, name := range []string{domain.ToolCheckoutPrepare, domain.ToolCartAdd, domain.ToolCartSub, domain.ToolCouponClaim} {
		metadata, err := registry.Metadata(name)
		if err != nil {
			t.Fatalf("Metadata(%q) returned error: %v", name, err)
		}
		if metadata.Risk != domain.RiskLow {
			t.Fatalf("%s Risk = %q, want %q", name, metadata.Risk, domain.RiskLow)
		}
		if metadata.RequireConfirmation {
			t.Fatalf("%s RequireConfirmation = true, want false", name)
		}
		if !metadata.WriteOperation {
			t.Fatalf("%s WriteOperation = false, want true", name)
		}
	}
}

func TestRegistryTimeoutDefaultsAndOverrides(t *testing.T) {
	defaultRegistry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})
	assertTimeout(t, defaultRegistry, domain.ToolProductSearch, 3)
	assertTimeout(t, defaultRegistry, domain.ToolCartAdd, 5)
	assertTimeout(t, defaultRegistry, domain.ToolOrderCancel, 5)

	customTimeout := config.ToolTimeoutConfig{
		QuerySeconds: 11,
		WriteSeconds: 17,
	}
	customRegistry := newTestRegistry(DefaultToolClients{}, customTimeout)
	assertTimeout(t, customRegistry, domain.ToolProductSearch, 11)
	assertTimeout(t, customRegistry, domain.ToolCartAdd, 17)
	assertTimeout(t, customRegistry, domain.ToolOrderCancel, 17)
}

func TestRegistryToolInfoSchemaDoesNotExposeUserID(t *testing.T) {
	ctx := context.Background()
	registry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})

	infos, err := registry.ToolInfos(ctx)
	if err != nil {
		t.Fatalf("ToolInfos returned error: %v", err)
	}
	if len(infos) != 20 {
		t.Fatalf("len(ToolInfos) = %d, want 20", len(infos))
	}

	for _, info := range infos {
		metadata, err := registry.Metadata(info.Name)
		if err != nil {
			t.Fatalf("Metadata(%q) returned error: %v", info.Name, err)
		}
		if info.Name != metadata.Name {
			t.Fatalf("ToolInfo name = %q, metadata name = %q", info.Name, metadata.Name)
		}
		if !openAIToolNamePattern.MatchString(info.Name) {
			t.Fatalf("ToolInfo(%q) name is not OpenAI-compatible", info.Name)
		}
		if info.Desc == "" {
			t.Fatalf("ToolInfo(%q) description should not be empty", info.Name)
		}
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatalf("marshal ToolInfo(%q): %v", info.Name, err)
		}
		if strings.Contains(strings.ToLower(string(raw)), "user_id") {
			t.Fatalf("ToolInfo(%q) schema exposes user_id: %s", info.Name, string(raw))
		}
	}
}

func TestRegistryReturnsToolsAndInfosByNamesInRequestedOrder(t *testing.T) {
	ctx := context.Background()
	registry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})

	tools, err := registry.ToolsByNames(domain.ToolOrderGet, domain.ToolProductSearch)
	if err != nil {
		t.Fatalf("ToolsByNames() error = %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("len(ToolsByNames) = %d, want 2", len(tools))
	}

	infos, err := registry.ToolInfosByNames(ctx, domain.ToolOrderGet, domain.ToolProductSearch)
	if err != nil {
		t.Fatalf("ToolInfosByNames() error = %v", err)
	}
	if len(infos) != 2 || infos[0].Name != domain.ToolOrderGet || infos[1].Name != domain.ToolProductSearch {
		t.Fatalf("infos = %+v", infos)
	}

	if _, err := registry.ToolsByNames("missing.tool"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("ToolsByNames missing error = %v, want ErrToolNotFound", err)
	}
}

func TestRegistryUnknownToolReturnsNotFound(t *testing.T) {
	registry := NewRegistry()

	if _, err := registry.Metadata("missing.tool"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Metadata missing error = %v, want ErrToolNotFound", err)
	}
	if _, err := registry.Tool("missing.tool"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Tool missing error = %v, want ErrToolNotFound", err)
	}
}

func TestRegistryWithoutCatalogHasNoPlaceholderTools(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.Tool(domain.ToolProductSearch); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Tool error = %v, want ErrToolNotFound", err)
	}
	if len(registry.AllMetadata()) != 0 {
		t.Fatalf("empty registry metadata = %#v, want none", registry.AllMetadata())
	}
}

func TestRegisteredToolExecutesThroughUnifiedAdapter(t *testing.T) {
	registry := newTestRegistry(DefaultToolClients{Product: &fakeProductQueryRPC{}}, config.ToolTimeoutConfig{})
	NewExecutor(registry)
	tool, err := registry.Tool(domain.ToolProductSearch)
	if err != nil {
		t.Fatalf("Tool returned error: %v", err)
	}

	_, err = tool.InvokableRun(context.Background(), `{}`)
	if errors.Is(err, ErrToolHandlerNotImplemented) {
		t.Fatalf("bound query tool still returns ErrToolHandlerNotImplemented")
	}
}

func TestRegistryHighRiskOrderAndCheckoutSchemasMatchRPCContracts(t *testing.T) {
	registry := newTestRegistry(DefaultToolClients{}, config.ToolTimeoutConfig{})

	checkoutTool, err := registry.Tool(domain.ToolCheckoutPrepare)
	if err != nil {
		t.Fatalf("checkout_prepare tool: %v", err)
	}
	checkoutInfo, err := checkoutTool.Info(context.Background())
	if err != nil {
		t.Fatalf("checkout_prepare info: %v", err)
	}
	checkoutSchema, err := checkoutInfo.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("checkout_prepare schema: %v", err)
	}
	if !containsString(checkoutSchema.Required, "order_items") {
		t.Fatalf("checkout_prepare required = %#v, want order_items", checkoutSchema.Required)
	}
	if _, ok := checkoutSchema.Properties.Get("order_items"); !ok {
		t.Fatal("checkout_prepare schema missing order_items")
	}
	if _, ok := checkoutSchema.Properties.Get("user_id"); ok {
		t.Fatal("checkout_prepare schema must not expose user_id")
	}

	orderTool, err := registry.Tool(domain.ToolOrderCreate)
	if err != nil {
		t.Fatalf("order_create tool: %v", err)
	}
	orderInfo, err := orderTool.Info(context.Background())
	if err != nil {
		t.Fatalf("order_create info: %v", err)
	}
	orderSchema, err := orderInfo.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("order_create schema: %v", err)
	}
	for _, name := range []string{"pre_order_id", "address_id", "payment_method"} {
		if !containsString(orderSchema.Required, name) {
			t.Fatalf("order_create required = %#v, want %s", orderSchema.Required, name)
		}
	}
	if _, ok := orderSchema.Properties.Get("user_id"); ok {
		t.Fatal("order_create schema must not expose user_id")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertTimeout(t *testing.T, registry *Registry, name string, want int64) {
	t.Helper()
	metadata, err := registry.Metadata(name)
	if err != nil {
		t.Fatalf("Metadata(%q) returned error: %v", name, err)
	}
	if metadata.TimeoutSeconds != want {
		t.Fatalf("%s TimeoutSeconds = %d, want %d", name, metadata.TimeoutSeconds, want)
	}
}

func expectedToolNames() []string {
	return []string{
		domain.ToolProductSearch,
		domain.ToolProductDetail,
		domain.ToolProductRecommend,
		domain.ToolInventoryGet,
		domain.ToolOrderGet,
		domain.ToolOrderList,
		domain.ToolCheckoutPrepare,
		domain.ToolCheckoutDetail,
		domain.ToolCartList,
		domain.ToolCartAdd,
		domain.ToolCartSub,
		domain.ToolCartDelete,
		domain.ToolCouponList,
		domain.ToolCouponDetail,
		domain.ToolCouponClaim,
		domain.ToolCouponMyList,
		domain.ToolCouponUsageList,
		domain.ToolCouponCalculate,
		domain.ToolOrderCreate,
		domain.ToolOrderCancel,
	}
}

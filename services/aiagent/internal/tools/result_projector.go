package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

var (
	ErrUnsupportedToolProjection = errors.New("unsupported tool result projection")
	ErrInvalidToolResultJSON     = errors.New("invalid tool result json")
)

var toolProjectionAllowed = map[string]bool{
	domain.ToolProductSearch:    true,
	domain.ToolProductDetail:    true,
	domain.ToolProductRecommend: true,
	domain.ToolInventoryGet:     true,
	domain.ToolOrderGet:         true,
	domain.ToolOrderList:        true,
	domain.ToolCheckoutPrepare:  true,
	domain.ToolCheckoutDetail:   true,
	domain.ToolCartList:         true,
	domain.ToolCartAdd:          true,
	domain.ToolCartSub:          true,
	domain.ToolCartDelete:       true,
	domain.ToolCouponList:       true,
	domain.ToolCouponDetail:     true,
	domain.ToolCouponClaim:      true,
	domain.ToolCouponMyList:     true,
	domain.ToolCouponUsageList:  true,
	domain.ToolCouponCalculate:  true,
}

// SupportsToolResultProjection 检查工具名称是否在白名单中
func SupportsToolResultProjection(toolName string) bool {
	return toolProjectionAllowed[toolName]
}

var entityKeyAliases = map[string]string{
	"product_id":      "product_ids",
	"product_ids":     "product_ids",
	"category_id":     "category_ids",
	"category_ids":    "category_ids",
	"order_id":        "order_ids",
	"order_ids":       "order_ids",
	"pre_order_id":    "pre_order_ids",
	"pre_order_ids":   "pre_order_ids",
	"checkout_id":     "checkout_ids",
	"checkout_ids":    "checkout_ids",
	"cart_item_id":    "cart_item_ids",
	"cart_item_ids":   "cart_item_ids",
	"coupon_id":       "coupon_ids",
	"coupon_ids":      "coupon_ids",
	"user_coupon_id":  "user_coupon_ids",
	"user_coupon_ids": "user_coupon_ids",
}

var stateKeyAliases = map[string]string{
	"status":          "status",
	"order_status":    "status",
	"coupon_status":   "status",
	"quantity":        "quantities",
	"quantities":      "quantities",
	"stock":           "stocks",
	"available_stock": "stocks",
}

func ProjectToolCallRef(toolName, status, summary, toolCallID string, createdAt time.Time, dataJSON []byte) (domain.ToolCallRef, error) {
	if !toolProjectionAllowed[toolName] {
		return domain.ToolCallRef{}, ErrUnsupportedToolProjection
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(dataJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return domain.ToolCallRef{}, fmt.Errorf("%w: %v", ErrInvalidToolResultJSON, err)
	}

	ref := domain.ToolCallRef{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Status:     status,
		Summary:    summary,
		EntityIDs:  make(map[string][]string),
		State:      make(map[string]any),
		CreatedAt:  createdAt,
	}
	entitySeen := make(map[string]map[string]bool)
	stateSeen := make(map[string]map[string]bool)
	walkProjectedJSON(decoded, ref.EntityIDs, entitySeen, ref.State, stateSeen)
	for key, values := range ref.EntityIDs {
		sort.Strings(values)
		ref.EntityIDs[key] = values
	}
	if len(ref.EntityIDs) == 0 {
		ref.EntityIDs = nil
	}
	if len(ref.State) == 0 {
		ref.State = nil
	}
	return ref, nil
}

func walkProjectedJSON(value any, entityIDs map[string][]string, entitySeen map[string]map[string]bool, state map[string]any, stateSeen map[string]map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if target, ok := entityKeyAliases[normalized]; ok {
				appendProjectedValues(entityIDs, entitySeen, target, nested)
			}
			if target, ok := stateKeyAliases[normalized]; ok {
				appendStateValues(state, stateSeen, target, nested)
			}
			walkProjectedJSON(nested, entityIDs, entitySeen, state, stateSeen)
		}
	case []any:
		for _, item := range typed {
			walkProjectedJSON(item, entityIDs, entitySeen, state, stateSeen)
		}
	}
}

func appendProjectedValues(destination map[string][]string, seen map[string]map[string]bool, key string, value any) {
	for _, item := range scalarStrings(value) {
		if item == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = make(map[string]bool)
		}
		if seen[key][item] {
			continue
		}
		seen[key][item] = true
		destination[key] = append(destination[key], item)
	}
}

func appendStateValues(destination map[string]any, seen map[string]map[string]bool, key string, value any) {
	values := scalarStrings(value)
	if len(values) == 0 {
		return
	}
	if key == "status" {
		if destination[key] == nil {
			destination[key] = values[0]
		}
		return
	}
	for _, item := range values {
		if item == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = make(map[string]bool)
		}
		if seen[key][item] {
			continue
		}
		seen[key][item] = true
		current, _ := destination[key].([]string)
		destination[key] = append(current, item)
	}
}

func scalarStrings(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case json.Number:
		return []string{typed.String()}
	case float64:
		return []string{strconv.FormatFloat(typed, 'f', -1, 64)}
	case int:
		return []string{strconv.Itoa(typed)}
	case int64:
		return []string{strconv.FormatInt(typed, 10)}
	case bool:
		return []string{strconv.FormatBool(typed)}
	case []any:
		var result []string
		for _, item := range typed {
			result = append(result, scalarStrings(item)...)
		}
		return result
	case []string:
		return typed
	case []int:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, strconv.Itoa(item))
		}
		return result
	case []int64:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, strconv.FormatInt(item, 10))
		}
		return result
	default:
		return nil
	}
}

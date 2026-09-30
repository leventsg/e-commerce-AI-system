// Package evaluation contains deterministic, local-only business fixtures.
// It has no service clients, credentials, database connections or network calls.
package evaluation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Object = map[string]any
type State struct {
	Products    map[string]Object `json:"products"`
	Inventory   map[string]int    `json:"inventory"`
	Cart        map[string]Object `json:"cart"`
	Coupons     map[string]Object `json:"coupons"`
	Claimed     map[string]bool   `json:"claimed_coupons"`
	CouponUsage []Object          `json:"coupon_usage"`
	Checkouts   map[string]Object `json:"checkouts"`
	Orders      map[string]Object `json:"orders"`
	Addresses   map[string]Object `json:"addresses"`
	Profile     Object            `json:"profile"`
	Audit       []Object          `json:"audit"`
	Sequence    int               `json:"sequence"`
}

func NewState(fixture json.RawMessage) (*State, error) {
	s := &State{Products: map[string]Object{}, Inventory: map[string]int{}, Cart: map[string]Object{}, Coupons: map[string]Object{}, Claimed: map[string]bool{}, CouponUsage: []Object{}, Checkouts: map[string]Object{}, Orders: map[string]Object{}, Addresses: map[string]Object{}, Profile: Object{"user_name": "Synthetic customer"}, Audit: []Object{}}
	if len(fixture) > 0 && string(fixture) != "null" {
		if err := json.Unmarshal(fixture, s); err != nil {
			return nil, fmt.Errorf("fixture: %w", err)
		}
	}
	if s.Products == nil || s.Inventory == nil || s.Cart == nil || s.Coupons == nil || s.Claimed == nil || s.Checkouts == nil || s.Orders == nil || s.Addresses == nil || s.Profile == nil {
		return nil, fmt.Errorf("fixture collections must not be null")
	}
	return s, nil
}
func clone(v Object) Object {
	b, _ := json.Marshal(v)
	var out Object
	_ = json.Unmarshal(b, &out)
	return out
}
func key(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func number(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		n, _ := x.Float64()
		return n
	}
	return 0
}
func rows(m map[string]Object) []Object {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r := []Object{}
	for _, k := range keys {
		r = append(r, clone(m[k]))
	}
	return r
}
func get(m map[string]Object, id string, user uint64) (Object, error) {
	v, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("object %q not found", id)
	}
	if owner := number(v["user_id"]); owner != 0 && uint64(owner) != user {
		return nil, fmt.Errorf("object access forbidden")
	}
	return v, nil
}
func (s *State) next(prefix string) string {
	for {
		s.Sequence++
		id := prefix + strconv.Itoa(s.Sequence)
		if s.Cart[id] == nil && s.Addresses[id] == nil && s.Checkouts[id] == nil && s.Orders[id] == nil {
			return id
		}
	}
}
func (s *State) total(items any) (float64, []Object, error) {
	a, ok := items.([]any)
	if !ok || len(a) == 0 {
		return 0, nil, fmt.Errorf("non-empty items required")
	}
	total := float64(0)
	out := []Object{}
	seen := map[string]bool{}
	for _, raw := range a {
		v, ok := raw.(map[string]any)
		if !ok {
			return 0, nil, fmt.Errorf("invalid item")
		}
		id := key(v["product_id"])
		q := number(v["quantity"])
		if q <= 0 || q > 100 || q != float64(int(q)) || seen[id] {
			return 0, nil, fmt.Errorf("invalid quantity or duplicate product")
		}
		p, ok := s.Products[id]
		if !ok {
			return 0, nil, fmt.Errorf("product not found")
		}
		seen[id] = true
		total += number(p["price"]) * q
		out = append(out, clone(v))
	}
	return total, out, nil
}

// Apply assumes schema validation has already occurred at the Eino boundary.
// All money uses the fixture's canonical minor currency unit.
func (s *State) Apply(tool string, a Object, user uint64) (Object, error) {
	if user == 0 {
		return nil, fmt.Errorf("authenticated user required")
	}
	switch tool {
	case "product_search", "product_recommend":
		list := []Object{}
		for _, p := range rows(s.Products) {
			if category := key(a["category"]); category != "" && category != key(p["category"]) {
				continue
			}
			if q := strings.ToLower(key(a["keyword"])); q != "" && !strings.Contains(strings.ToLower(key(p["name"])+" "+key(p["description"])), q) {
				continue
			}
			price := number(p["price"])
			if rangeValue, ok := a["price"].(map[string]any); ok {
				if price < number(rangeValue["min"]) || (number(rangeValue["max"]) > 0 && price > number(rangeValue["max"])) {
					continue
				}
			}
			if v, ok := a["min_price"]; ok && price < number(v) {
				continue
			}
			if v, ok := a["max_price"]; ok && price > number(v) {
				continue
			}
			list = append(list, p)
		}
		return Object{"products": list, "total": len(list)}, nil
	case "product_detail":
		p, e := get(s.Products, key(a["product_id"]), user)
		return Object{"product": p}, e
	case "inventory_get":
		id := key(a["product_id"])
		if _, ok := s.Products[id]; !ok {
			return nil, fmt.Errorf("product not found")
		}
		return Object{"product_id": a["product_id"], "inventory": s.Inventory[id]}, nil
	case "cart_list":
		list := []Object{}
		for _, v := range rows(s.Cart) {
			if owner := number(v["user_id"]); owner == 0 || uint64(owner) == user {
				list = append(list, v)
			}
		}
		return Object{"items": list, "total": len(list)}, nil
	case "cart_add":
		id := key(a["product_id"])
		if _, ok := s.Products[id]; !ok {
			return nil, fmt.Errorf("product not found")
		}
		q := number(a["quantity"])
		if _, ok := a["quantity"]; !ok {
			q = 1
		}
		if q < 1 || q > 100 {
			return nil, fmt.Errorf("quantity outside 1..100")
		}
		for _, v := range s.Cart {
			if key(v["product_id"]) == id && (number(v["user_id"]) == 0 || uint64(number(v["user_id"])) == user) {
				v["quantity"] = number(v["quantity"]) + q
				return clone(v), nil
			}
		}
		cid := s.next("")
		v := Object{"cart_item_id": s.Sequence, "product_id": a["product_id"], "quantity": q, "user_id": user}
		s.Cart[cid] = v
		return clone(v), nil
	case "cart_sub", "cart_delete":
		id := key(a["cart_item_id"])
		v, e := get(s.Cart, id, user)
		if e != nil {
			return nil, e
		}
		if tool == "cart_delete" {
			delete(s.Cart, id)
			return clone(v), nil
		}
		q := number(a["quantity"])
		if _, ok := a["quantity"]; !ok {
			q = 1
		}
		if q <= 0 || q >= number(v["quantity"]) {
			return nil, fmt.Errorf("quantity must leave at least one item")
		}
		v["quantity"] = number(v["quantity"]) - q
		return clone(v), nil
	case "coupon_list":
		return Object{"coupons": rows(s.Coupons)}, nil
	case "coupon_detail":
		v, e := get(s.Coupons, key(a["coupon_id"]), user)
		return Object{"coupon": v}, e
	case "coupon_claim":
		id := key(a["coupon_id"])
		v, e := get(s.Coupons, id, user)
		if e != nil {
			return nil, e
		}
		if s.Claimed[id] {
			return nil, fmt.Errorf("coupon already claimed")
		}
		if v["expired"] == true {
			return nil, fmt.Errorf("coupon expired")
		}
		s.Claimed[id] = true
		return Object{"coupon_id": id, "coupon": v}, nil
	case "coupon_my_list":
		list := []Object{}
		for _, v := range rows(s.Coupons) {
			if s.Claimed[key(v["coupon_id"])] {
				list = append(list, v)
			}
		}
		return Object{"coupons": list}, nil
	case "coupon_usage_list":
		list := []Object{}
		for _, v := range s.CouponUsage {
			if owner := number(v["user_id"]); owner == 0 || uint64(owner) == user {
				list = append(list, clone(v))
			}
		}
		return Object{"usages": list}, nil
	case "coupon_calculate":
		total, _, e := s.total(a["items"])
		if e != nil {
			return nil, e
		}
		c, e := get(s.Coupons, key(a["coupon_id"]), user)
		if e != nil {
			return nil, e
		}
		usable := total >= number(c["min_spend"]) && c["expired"] != true
		discount := float64(0)
		if usable {
			discount = number(c["discount"])
			if discount > total {
				discount = total
			}
		}
		return Object{"is_usable": usable, "original_amount": total, "discount_amount": discount, "final_amount": total - discount}, nil
	case "checkout_prepare":
		total, items, e := s.total(a["order_items"])
		if e != nil {
			return nil, e
		}
		for _, v := range items {
			if s.Inventory[key(v["product_id"])] < int(number(v["quantity"])) {
				return nil, fmt.Errorf("insufficient inventory")
			}
		}
		cid := key(a["coupon_id"])
		discount := float64(0)
		if cid != "" {
			c, e := get(s.Coupons, cid, user)
			if e != nil {
				return nil, e
			}
			if !s.Claimed[cid] || c["expired"] == true || total < number(c["min_spend"]) {
				return nil, fmt.Errorf("coupon not usable")
			}
			discount = number(c["discount"])
			if discount > total {
				discount = total
			}
		}
		for _, v := range items {
			s.Inventory[key(v["product_id"])] -= int(number(v["quantity"]))
		}
		id := s.next("pre_")
		v := Object{"pre_order_id": id, "user_id": user, "order_items": items, "total_amount": total - discount, "coupon_id": cid, "status": "prepared"}
		s.Checkouts[id] = v
		return clone(v), nil
	case "checkout_detail":
		v, e := get(s.Checkouts, key(a["pre_order_id"]), user)
		return clone(v), e
	case "order_create":
		p, e := get(s.Checkouts, key(a["pre_order_id"]), user)
		if e != nil {
			return nil, e
		}
		if p["status"] != "prepared" {
			return nil, fmt.Errorf("checkout already consumed")
		}
		if _, e = get(s.Addresses, key(a["address_id"]), user); e != nil {
			return nil, e
		}
		id := s.next("order_")
		v := clone(p)
		v["order_id"] = id
		v["address_id"] = a["address_id"]
		v["status"] = "pending_payment"
		s.Orders[id] = v
		p["status"] = "ordered"
		if cid := key(p["coupon_id"]); cid != "" {
			s.Claimed[cid] = false
			s.CouponUsage = append(s.CouponUsage, Object{"coupon_id": cid, "order_id": id})
		}
		return clone(v), nil
	case "order_get":
		v, e := get(s.Orders, key(a["order_id"]), user)
		return Object{"order": v}, e
	case "order_list":
		list := []Object{}
		for id := range s.Orders {
			v, e := get(s.Orders, id, user)
			if e == nil {
				list = append(list, clone(v))
			}
		}
		sort.Slice(list, func(i, j int) bool { return key(list[i]["order_id"]) < key(list[j]["order_id"]) })
		return Object{"orders": list, "total": len(list)}, nil
	case "order_cancel":
		v, e := get(s.Orders, key(a["order_id"]), user)
		if e != nil {
			return nil, e
		}
		if v["status"] != "pending_payment" && v["status"] != "pending" {
			return nil, fmt.Errorf("order cannot be cancelled")
		}
		v["status"] = "cancelled"
		if items, ok := v["order_items"].([]any); ok {
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				s.Inventory[key(item["product_id"])] += int(number(item["quantity"]))
			}
		}
		return clone(v), nil
	case "user_get":
		return Object{"user": clone(s.Profile)}, nil
	case "user_update":
		if key(a["user_name"]) == "" && key(a["avatar_url"]) == "" {
			return nil, fmt.Errorf("user_name or avatar_url required")
		}
		for k, v := range a {
			s.Profile[k] = v
		}
		return clone(s.Profile), nil
	case "address_list":
		list := []Object{}
		for _, v := range rows(s.Addresses) {
			if owner := number(v["user_id"]); owner == 0 || uint64(owner) == user {
				list = append(list, v)
			}
		}
		return Object{"addresses": list}, nil
	case "address_get":
		v, e := get(s.Addresses, key(a["address_id"]), user)
		return Object{"address": v}, e
	case "address_add", "address_update":
		id := key(a["address_id"])
		if tool == "address_update" {
			if _, e := get(s.Addresses, id, user); e != nil {
				return nil, e
			}
		} else {
			id = s.next("")
		}
		v := clone(a)
		n, _ := strconv.Atoi(id)
		v["address_id"] = n
		v["user_id"] = user
		if v["is_default"] == true {
			for _, old := range s.Addresses {
				if owner := number(old["user_id"]); owner == 0 || uint64(owner) == user {
					old["is_default"] = false
				}
			}
		}
		s.Addresses[id] = v
		return Object{"address": clone(v)}, nil
	case "address_delete":
		id := key(a["address_id"])
		if _, e := get(s.Addresses, id, user); e != nil {
			return nil, e
		}
		delete(s.Addresses, id)
		return Object{"address_id": a["address_id"]}, nil
	}
	return nil, fmt.Errorf("unsupported simulated tool %q", tool)
}

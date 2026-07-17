package argx

import "testing"

func TestSanitizeMapKeysRemovesSensitiveKeysRecursively(t *testing.T) {
	input := map[string]any{
		" user_id ":  999,
		"product_id": 12,
		"profile": map[string]any{
			"TOKEN": "secret",
			"keep":  "value",
		},
		"items": []any{
			map[string]any{
				"auth":   "bearer",
				"sku_id": 7,
			},
			"plain",
		},
	}

	cleaned := SanitizeMapKeys(input, []string{"user_id", "token", "auth"})

	assertNoKey(t, cleaned, "user_id")
	assertNoKey(t, cleaned, "TOKEN")
	assertNoKey(t, cleaned, "auth")
	if cleaned["product_id"] != 12 {
		t.Fatalf("product_id = %#v, want 12", cleaned["product_id"])
	}
	profile := cleaned["profile"].(map[string]any)
	if profile["keep"] != "value" {
		t.Fatalf("profile.keep = %#v, want value", profile["keep"])
	}
	items := cleaned["items"].([]any)
	item := items[0].(map[string]any)
	if item["sku_id"] != 7 {
		t.Fatalf("items[0].sku_id = %#v, want 7", item["sku_id"])
	}
}

func TestSanitizeMapKeysDoesNotMutateInput(t *testing.T) {
	input := map[string]any{
		"user_id": 999,
		"profile": map[string]any{
			"token": "secret",
		},
	}

	cleaned := SanitizeMapKeys(input, []string{"user_id", "token"})
	cleaned["new"] = "value"
	cleanedProfile := cleaned["profile"].(map[string]any)
	cleanedProfile["keep"] = "value"

	if input["user_id"] != 999 {
		t.Fatalf("input user_id changed: %#v", input)
	}
	inputProfile := input["profile"].(map[string]any)
	if inputProfile["token"] != "secret" {
		t.Fatalf("input nested token changed: %#v", inputProfile)
	}
	if _, ok := input["new"]; ok {
		t.Fatalf("cleaned top-level mutation leaked into input: %#v", input)
	}
	if _, ok := inputProfile["keep"]; ok {
		t.Fatalf("cleaned nested mutation leaked into input: %#v", inputProfile)
	}
}

func TestSanitizeMapKeysRemovesAliasesFromTypedCollections(t *testing.T) {
	input := map[string]any{
		"userId": "999",
		"headers": map[string]string{
			"Authorization": "Bearer secret",
			"keep":          "value",
		},
		"items": []map[string]any{
			{"access-token": "secret", "product_id": 12},
		},
	}

	cleaned := SanitizeMapKeys(input, []string{"user_id", "authorization", "access_token"})
	if _, ok := cleaned["userId"]; ok {
		t.Fatalf("camel-case user ID leaked: %#v", cleaned)
	}
	headers := cleaned["headers"].(map[string]any)
	if _, ok := headers["Authorization"]; ok || headers["keep"] != "value" {
		t.Fatalf("typed headers = %#v", headers)
	}
	items := cleaned["items"].([]any)
	item := items[0].(map[string]any)
	if _, ok := item["access-token"]; ok || item["product_id"] != 12 {
		t.Fatalf("typed items = %#v", items)
	}
}

func assertNoKey(t *testing.T, value any, banned string) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == banned {
				t.Fatalf("key %q leaked in %#v", banned, typed)
			}
			assertNoKey(t, nested, banned)
		}
	case []any:
		for _, item := range typed {
			assertNoKey(t, item, banned)
		}
	}
}

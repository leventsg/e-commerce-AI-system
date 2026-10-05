//go:build ai_observability

package observability

import (
	"strings"
	"testing"
)

func TestPrivacyDefaultsAndCredentials(t *testing.T) {
	raw := `{"content":"deliver to 123 Main Street phone 13812345678","api_key":"sk-secret","nested":{"access_token":"token-secret"}}`
	p := Policy{Salt: "test-salt"}
	if got := p.Content(raw); strings.Contains(got, "Main") || strings.Contains(got, "secret") {
		t.Fatal(got)
	}
	p.Mode = ModeSynthetic
	got := p.Content(raw)
	if strings.Contains(got, "sk-secret") || strings.Contains(got, "token-secret") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "Main Street") {
		t.Fatal("synthetic content should remain useful")
	}
	if p.UserID(42) == "42" || p.UserID(42) != p.UserID(42) || p.UserID(42) == p.UserID(43) {
		t.Fatal("invalid pseudonym")
	}
}

func TestRedactedModeStripsCredentialsAndPII(t *testing.T) {
	p := Policy{Salt: "salt", Mode: ModeRedacted}
	raw := `{"user":"张先生","content":"请联系我 13812345678 或 a@b.com，地址 010-12345678","api_key":"sk-abc","nested":{"access_token":"t-secret"}}`
	got := p.Content(raw)
	for _, leak := range []string{"13812345678", "a@b.com", "010-12345678", "sk-abc", "t-secret"} {
		if strings.Contains(got, leak) {
			t.Fatalf("redacted content leaked %q: %s", leak, got)
		}
	}
	// Useful non-PII content is retained, unlike summary mode.
	if !strings.Contains(got, "请联系我") {
		t.Fatalf("redacted content lost useful text: %s", got)
	}
}

func TestRedactedModeHandlesNestedJSONStrings(t *testing.T) {
	p := Policy{Salt: "salt", Mode: ModeRedacted}
	raw := `{"tool":"order_get","arguments":"{\"phone\":\"13812345678\",\"token\":\"t-secret\"}"}`
	got := p.Content(raw)
	if strings.Contains(got, "13812345678") || strings.Contains(got, "t-secret") {
		t.Fatalf("nested JSON leaked: %s", got)
	}
}

func TestSummaryModeOmitsText(t *testing.T) {
	p := Policy{Salt: "salt"} // default mode is summary
	got := p.Content(`{"content":"hello world","phone":"13812345678"}`)
	if strings.Contains(got, "hello") || strings.Contains(got, "13812345678") {
		t.Fatalf("summary mode leaked content: %s", got)
	}
	if !strings.Contains(got, "content_omitted") {
		t.Fatalf("summary mode missing omit marker: %s", got)
	}
}

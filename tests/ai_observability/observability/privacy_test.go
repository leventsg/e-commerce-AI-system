//go:build ai_observability

package observability

import (
 "strings"
 "testing"
)

func TestPrivacyDefaultsAndCredentials(t *testing.T) {
 raw := `{"content":"deliver to 123 Main Street phone 13812345678","api_key":"sk-secret","nested":{"access_token":"token-secret"}}`
 p := Policy{Salt:"test-salt"}
 if got:=p.Content(raw); strings.Contains(got,"Main") || strings.Contains(got,"secret") { t.Fatal(got) }
 p.Synthetic=true
 got:=p.Content(raw)
 if strings.Contains(got,"sk-secret") || strings.Contains(got,"token-secret") {t.Fatal(got)}
 if !strings.Contains(got,"Main Street") {t.Fatal("synthetic content should remain useful")}
 if p.UserID(42)=="42" || p.UserID(42)!=p.UserID(42) || p.UserID(42)==p.UserID(43) {t.Fatal("invalid pseudonym")}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeromicro/go-zero/zrpc"
)

func TestNormalizeURLPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "/"},
		{name: "clean repeated slash", in: "/douyin//coupon/list", want: "/douyin/coupon/list"},
		{name: "clean current dir", in: "/douyin/./coupon/list", want: "/douyin/coupon/list"},
		{name: "add leading slash", in: "douyin/coupon/list", want: "/douyin/coupon/list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeURLPath(tt.in); got != tt.want {
				t.Fatalf("normalizeURLPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWrapperAuthMiddlewareAllowsNormalizedWhitePath(t *testing.T) {
	middleware := WrapperAuthMiddleware(zrpc.RpcClientConf{}, []string{"/douyin/coupon/list"}, nil)
	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/douyin/coupon/list" {
			t.Fatalf("request path not normalized, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/douyin//coupon/list", nil)
	rr := httptest.NewRecorder()

	handler(rr, req)

	if !called {
		t.Fatal("expected white path request to bypass auth")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("unexpected status code %d", rr.Code)
	}
}

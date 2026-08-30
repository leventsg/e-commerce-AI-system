package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/config"
)

func TestInternalCallMiddlewareAllowsLoopbackRemoteAddr(t *testing.T) {
	middleware := InternalCallMiddleware(config.InternalCallConfig{
		AllowedIPs: []string{"127.0.0.1", "::1"},
	})
	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodPost, "/douyin/order/internal/create", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	handler(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was not called for loopback remote address")
	}
}

func TestInternalCallMiddlewareRejectsSpoofedForwardedHeader(t *testing.T) {
	middleware := InternalCallMiddleware(config.InternalCallConfig{
		AllowedIPs: []string{"127.0.0.1"},
	})
	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodPost, "/douyin/order/internal/create", nil)
	req.RemoteAddr = "203.0.113.10:45678"
	req.Header.Set("X-Real-IP", "127.0.0.1")
	recorder := httptest.NewRecorder()
	handler(recorder, req)

	if called {
		t.Fatal("handler was called for spoofed forwarded header")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestInternalCallMiddlewareAllowsCIDR(t *testing.T) {
	middleware := InternalCallMiddleware(config.InternalCallConfig{
		AllowedIPs: []string{"10.0.0.0/8"},
	})
	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodPost, "/douyin/order/internal/cancel", nil)
	req.RemoteAddr = "10.2.3.4:45678"
	handler(httptest.NewRecorder(), req)

	if !called {
		t.Fatal("handler was not called for CIDR-matched remote address")
	}
}

package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testPayload struct {
	ID string `json:"id"`
}

func TestDoJSONEnvelopeDecodesSuccessfulEnvelope(t *testing.T) {
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"id":"request-1"`) {
			t.Fatalf("request body = %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"id":"response-1"}}`)
	}))
	defer server.Close()

	var out testPayload
	err := DoJSONEnvelope(context.Background(), http.MethodPost, server.URL, testPayload{ID: "request-1"}, &out)
	if err != nil {
		t.Fatalf("DoJSONEnvelope: %v", err)
	}
	if out.ID != "response-1" {
		t.Fatalf("out.ID=%q", out.ID)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type=%q", gotContentType)
	}
}

func TestDoJSONEnvelopeRejectsHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad upstream", http.StatusBadGateway)
	}))
	defer server.Close()

	var out testPayload
	err := DoJSONEnvelope(context.Background(), http.MethodPost, server.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "http status 502") {
		t.Fatalf("err=%v, want http status error", err)
	}
}

func TestDoJSONEnvelopeRejectsBusinessCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":400,"msg":"business failed"}`)
	}))
	defer server.Close()

	var out testPayload
	err := DoJSONEnvelope(context.Background(), http.MethodPost, server.URL, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "code=400") {
		t.Fatalf("err=%v, want business code error", err)
	}
}

func TestDoJSONEnvelopeRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":"`+strings.Repeat("x", 32)+`"}`)
	}))
	defer server.Close()

	var out string
	err := DoJSONEnvelope(context.Background(), http.MethodPost, server.URL, nil, &out, WithMaxResponseBytes(16))
	if err == nil || !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("err=%v, want response too large", err)
	}
}

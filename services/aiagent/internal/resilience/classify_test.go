package resilience

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

type fakeStatusError struct {
	HTTPStatusCode int
	msg            string
}

func (e *fakeStatusError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("http status %d", e.HTTPStatusCode)
}

func (e *fakeStatusError) Unwrap() error { return nil }

type fakeNetError struct{ timeout bool }

func (e *fakeNetError) Error() string   { return "fake net error" }
func (e *fakeNetError) Timeout() bool   { return e.timeout }
func (e *fakeNetError) Temporary() bool { return e.timeout }

func TestClassify_Nil(t *testing.T) {
	if c := Classify(nil); c != ClassSuccess {
		t.Fatalf("expected success, got %q", c)
	}
}

func TestClassify_Context(t *testing.T) {
	if c := Classify(context.DeadlineExceeded); c != ClassTimeout {
		t.Fatalf("expected timeout, got %q", c)
	}
	if c := Classify(context.Canceled); c != ClassCanceled {
		t.Fatalf("expected canceled, got %q", c)
	}
}

func TestClassify_HTTPStatusCodes(t *testing.T) {
	cases := []struct {
		code int
		want ErrorClass
	}{
		{408, ClassTimeout},
		{429, ClassRateLimited},
		{500, ClassServerError},
		{502, ClassServerError},
		{503, ClassServerError},
		{504, ClassServerError},
		{401, ClassAuthOrConfig},
		{403, ClassAuthOrConfig},
		{404, ClassAuthOrConfig},
		{400, ClassBadRequest},
		{413, ClassBadRequest},
		{422, ClassBadRequest},
	}
	for _, c := range cases {
		if got := Classify(&fakeStatusError{HTTPStatusCode: c.code}); got != c.want {
			t.Errorf("code %d: expected %q, got %q", c.code, c.want, got)
		}
	}
}

func TestClassify_HTTPStatusThroughWrap(t *testing.T) {
	// 状态码错误被 %w 包裹后仍能抽取
	err := fmt.Errorf("request failed: %w", &fakeStatusError{HTTPStatusCode: 503})
	if got := Classify(err); got != ClassServerError {
		t.Fatalf("expected server error through wrap, got %q", got)
	}
}

func TestClassify_NetError(t *testing.T) {
	if got := Classify(&fakeNetError{timeout: true}); got != ClassTimeout {
		t.Fatalf("expected timeout for net timeout, got %q", got)
	}
	if got := Classify(&fakeNetError{timeout: false}); got != ClassNetwork {
		t.Fatalf("expected network for non-timeout net error, got %q", got)
	}
}

func TestClassify_StringFallback(t *testing.T) {
	cases := []struct {
		msg  string
		want ErrorClass
	}{
		{"401 unauthorized", ClassAuthOrConfig},
		{"invalid api key", ClassAuthOrConfig},
		{"429 too many requests", ClassRateLimited},
		{"request timed out", ClassTimeout},
		{"500 internal server error", ClassServerError},
		{"context length exceeded", ClassBadRequest},
	}
	for _, c := range cases {
		if got := Classify(errors.New(c.msg)); got != c.want {
			t.Errorf("msg %q: expected %q, got %q", c.msg, c.want, got)
		}
	}
}

func TestErrorClass_Retryable(t *testing.T) {
	retryable := []ErrorClass{ClassTimeout, ClassRateLimited, ClassServerError, ClassNetwork}
	for _, c := range retryable {
		if !c.Retryable() {
			t.Errorf("expected %q to be retryable", c)
		}
	}
	nonRetryable := []ErrorClass{ClassSuccess, ClassAuthOrConfig, ClassBadRequest, ClassCanceled, ClassUnknown}
	for _, c := range nonRetryable {
		if c.Retryable() {
			t.Errorf("expected %q to be non-retryable", c)
		}
	}
}

var _ net.Error = (*fakeNetError)(nil)

package eino

import (
	"context"
	"errors"
	"testing"
)

func TestErrorReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: "model_timeout"},
		{name: "authentication", err: errors.New("request failed with status 401"), want: "model_auth_or_config_failed"},
		{name: "empty response", err: ErrEmptyModelResponse, want: "model_empty_response"},
		{name: "generic", err: errors.New("connection reset"), want: "model_request_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorReason(tt.err); got != tt.want {
				t.Fatalf("ErrorReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

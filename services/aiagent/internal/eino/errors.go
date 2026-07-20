package eino

import (
	"context"
	"errors"
	"net"
	"strings"
)

var ErrEmptyModelResponse = errors.New("empty_model_response")

func ErrorReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrEmptyModelResponse) {
		return "model_empty_response"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "model_timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "model_timeout"
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"401", "403", "unauthorized", "invalid api key", "authentication", "api key", "provider is required", "model is required", "unsupported provider", "base url"} {
		if strings.Contains(message, marker) {
			return "model_auth_or_config_failed"
		}
	}
	return "model_request_failed"
}

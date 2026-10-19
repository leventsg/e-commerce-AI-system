package resilience

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
)

// ErrorClass 模型调用错误的粗粒度分类，用于决定「是否重试」与「熔断/告警」策略。
type ErrorClass string

const (
	// ClassSuccess 表示无错误。
	ClassSuccess ErrorClass = ""
	// ClassTimeout 客户端/网络超时，可重试（瞬时抖动）。
	ClassTimeout ErrorClass = "model_timeout"
	// ClassRateLimited 429 限流，可重试（退避更激进）。
	ClassRateLimited ErrorClass = "model_rate_limited"
	// ClassServerError 5xx 服务端临时故障，可重试。
	ClassServerError ErrorClass = "model_server_error"
	// ClassNetwork 其他网络错误（连接拒绝/EOF/DNS），可重试。
	ClassNetwork ErrorClass = "model_network_error"
	// ClassAuthOrConfig 401/403/404 鉴权或配置错误，不可重试，需告警。
	ClassAuthOrConfig ErrorClass = "model_auth_or_config_failed"
	// ClassBadRequest 400/413/422 请求构造错误，不可重试（我方 bug，需修码）。
	ClassBadRequest ErrorClass = "model_bad_request"
	// ClassCanceled 调用方主动取消，不可重试。
	ClassCanceled ErrorClass = "model_canceled"
	// ClassUnknown 无法归类的错误，保守起见不重试。
	ClassUnknown ErrorClass = "model_unknown_error"
)

// Retryable 报告该类错误是否值得重试。
func (c ErrorClass) Retryable() bool {
	switch c {
	case ClassTimeout, ClassRateLimited, ClassServerError, ClassNetwork:
		return true
	default:
		return false
	}
}

// Classify 将模型调用错误归类为 ErrorClass。
//
// 判定顺序：上下文取消/超时 → HTTP 状态码（反射抽取，适配带 HTTPStatusCode
// 字段的 openai.APIError / RequestError 等）→ net.Error → 字符串兜底。
func Classify(err error) ErrorClass {
	if err == nil {
		return ClassSuccess
	}
	if errors.Is(err, context.Canceled) {
		return ClassCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTimeout
	}

	if code := httpStatusCode(err); code > 0 {
		return classFromHTTPStatus(code)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ClassTimeout
		}
		return ClassNetwork
	}

	// 字符串兜底：错误被包裹丢失类型时，仍能从消息识别关键类别。
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "401", "403", "unauthorized", "invalid api key", "authentication", "api key", "insufficient_quota", "permission"):
		return ClassAuthOrConfig
	case containsAny(msg, "429", "rate limit", "too many requests", "quota exceeded"):
		return ClassRateLimited
	case containsAny(msg, "408", "timeout", "timed out", "deadline exceeded"):
		return ClassTimeout
	case containsAny(msg, "500", "502", "503", "504", "internal server error", "service unavailable", "bad gateway", "overloaded"):
		return ClassServerError
	case containsAny(msg, "413", "context length", "too many tokens", "maximum context", "prompt is too long"):
		return ClassBadRequest
	case containsAny(msg, "404", "not found", "no such model", "model not found"):
		return ClassAuthOrConfig
	case containsAny(msg, "400", "422", "invalid", "bad request"):
		return ClassBadRequest
	default:
		return ClassUnknown
	}
}

func containsAny(s string, markers ...string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// classFromHTTPStatus 根据 HTTP 状态码归类。
func classFromHTTPStatus(code int) ErrorClass {
	switch {
	case code == 408:
		return ClassTimeout
	case code == 429:
		return ClassRateLimited
	case code >= 500 && code <= 599:
		return ClassServerError
	case code == 401 || code == 403 || code == 404:
		return ClassAuthOrConfig
	case code == 400 || code == 413 || code == 422 || code == 409:
		return ClassBadRequest
	default:
		return ClassUnknown
	}
}

// httpStatusCode 从错误链中抽取 HTTP 状态码。适配带 int 类型 HTTPStatusCode
// 字段的错误结构体（如 openai.APIError / openai.RequestError），避免强耦合具体包。
func httpStatusCode(err error) int {
	for err != nil {
		if code := statusCodeOf(err); code > 0 {
			return code
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return 0
		}
		err = u.Unwrap()
	}
	return 0
}

func statusCodeOf(err error) int {
	if err == nil {
		return 0
	}
	v := reflect.ValueOf(err)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0
	}
	f := v.FieldByName("HTTPStatusCode")
	if !f.IsValid() {
		return 0
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(f.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(f.Uint())
	default:
		return 0
	}
}

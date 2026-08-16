package tools_helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

// 默认分页参数
const (
	defaultQueryPage     = int32(1)
	defaultQueryPageSize = int32(10)
	maxQueryPageSize     = int32(100)
)

// 错误信息
var ErrInvalidToolArguments = errors.New("invalid ai tool arguments")
var ErrQueryRPCUnavailable = errors.New("query rpc unavailable")
var ErrToolExecutionContext = errors.New("trusted tool execution context missing")

type ToolExecutionContext struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ToolCallID     string
	ClientIP       string
	RunID          string
	CheckpointID   string
}

type toolExecutionContextKey struct{}

func WithToolExecutionContext(ctx context.Context, execution ToolExecutionContext) context.Context {
	return context.WithValue(ctx, toolExecutionContextKey{}, execution)
}

func ToolExecutionFromContext(ctx context.Context) (ToolExecutionContext, bool) {
	execution, ok := ctx.Value(toolExecutionContextKey{}).(ToolExecutionContext)
	return execution, ok
}

func ExecuteRequestFromContext(execution ToolExecutionContext, toolName string, arguments map[string]any) core.ExecuteRequest {
	return core.ExecuteRequest{
		UserID:         execution.UserID,
		ConversationID: execution.ConversationID,
		MessageID:      execution.MessageID,
		ToolCallID:     execution.ToolCallID,
		ClientIP:       execution.ClientIP,
		ToolName:       toolName,
		Arguments:      arguments,
	}
}

// 检查参数中是否包含必需的字符串参数
func RequiredStringArgument(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok {
		return "", InvalidArgument(name, "is required")
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", InvalidArgument(name, "must be a non-empty string")
	}
	return strings.TrimSpace(text), nil
}

func OptionalStringArgument(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", InvalidArgument(name, "must be a string")
	}
	return strings.TrimSpace(text), nil
}

func OptionalStringListArgument(args map[string]any, name string) ([]string, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '，'
		})
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if item := strings.TrimSpace(part); item != "" {
				result = append(result, item)
			}
		}
		return result, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, InvalidArgument(name, "must contain only non-empty strings")
			}
			result = append(result, strings.TrimSpace(text))
		}
		return result, nil
	default:
		return nil, InvalidArgument(name, "must be a string or string array")
	}
}

func RequiredInt64Argument(args map[string]any, name string) (int64, error) {
	value, ok := args[name]
	if !ok {
		return 0, InvalidArgument(name, "is required")
	}
	parsed, err := integerValue(value)
	if err != nil {
		return 0, InvalidArgument(name, err.Error())
	}
	return parsed, nil
}

func OptionalInt64Argument(args map[string]any, name string, fallback int64) (int64, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback, nil
	}
	parsed, err := integerValue(value)
	if err != nil {
		return 0, InvalidArgument(name, err.Error())
	}
	return parsed, nil
}

func integerValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case uint:
		if uint64(typed) > math.MaxInt64 {
			return 0, errors.New("is out of range")
		}
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		if typed > math.MaxInt64 {
			return 0, errors.New("is out of range")
		}
		return int64(typed), nil
	case float64:
		if math.Trunc(typed) != typed || typed < math.MinInt64 || typed > math.MaxInt64 {
			return 0, errors.New("must be an integer")
		}
		return int64(typed), nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, errors.New("must be an integer")
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, errors.New("must be an integer")
		}
		return parsed, nil
	default:
		return 0, errors.New("must be an integer")
	}
}

func PositiveInt32(value int64, name string) (int32, error) {
	if value <= 0 || value > math.MaxInt32 {
		return 0, InvalidArgument(name, "must be a positive 32-bit integer")
	}
	return int32(value), nil
}

func PositiveUint32(value int64, name string) (uint32, error) {
	if value <= 0 || value > math.MaxUint32 {
		return 0, InvalidArgument(name, "must be a positive 32-bit integer")
	}
	return uint32(value), nil
}

func AuthenticatedUserID32(userID uint64) (int32, error) {
	if userID == 0 || userID > math.MaxInt32 {
		return 0, fmt.Errorf("%w: authenticated user_id is invalid", ErrInvalidToolArguments)
	}
	return int32(userID), nil
}

// 解析分页参数
func QueryPagination(args map[string]any) (page, pageSize int32, err error) {
	pageValue, err := OptionalInt64Argument(args, "page", int64(defaultQueryPage))
	if err != nil {
		return 0, 0, err
	}
	pageSizeValue, err := OptionalInt64Argument(args, "page_size", int64(defaultQueryPageSize))
	if err != nil {
		return 0, 0, err
	}
	page, err = PositiveInt32(pageValue, "page")
	if err != nil {
		return 0, 0, err
	}
	pageSize, err = PositiveInt32(pageSizeValue, "page_size")
	if err != nil {
		return 0, 0, err
	}
	if pageSize > maxQueryPageSize {
		pageSize = maxQueryPageSize
	}
	return page, pageSize, nil
}

func InvalidArgument(name, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidToolArguments, name, reason)
}

// 验证 RPC 响应
func ValidateRPCResponse(operation string, response any, statusCode int64, statusMessage string) error {
	if response == nil {
		return fmt.Errorf("%w: %s returned nil response", ErrQueryRPCUnavailable, operation)
	}
	if statusCode != 0 {
		message := strings.TrimSpace(statusMessage)
		if message == "" {
			message = "business request failed"
		}
		return fmt.Errorf("%s failed: %s", operation, message)
	}
	return nil
}

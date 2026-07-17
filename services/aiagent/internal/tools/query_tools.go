package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

// 默认分页参数
const (
	defaultQueryPage     = int32(1)
	defaultQueryPageSize = int32(10)
	maxQueryPageSize     = int32(100)
)

// 错误信息
var (
	ErrInvalidToolArguments = errors.New("invalid ai tool arguments")
	ErrQueryRPCUnavailable  = errors.New("query rpc unavailable")
	ErrToolExecutionContext = errors.New("trusted tool execution context missing")
	ErrQueryToolExecution   = errors.New("query tool execution failed")
)

type ToolExecutionContext struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ClientIP       string
}

type toolExecutionContextKey struct{}

func WithToolExecutionContext(ctx context.Context, execution ToolExecutionContext) context.Context {
	return context.WithValue(ctx, toolExecutionContextKey{}, execution)
}

// 包含所有查询工具的 RPC 客户端接口
type QueryToolClients struct {
	Product   ProductQueryRPC
	Inventory InventoryQueryRPC
	Order     OrderQueryRPC
	Cart      CartQueryRPC
	Coupon    CouponQueryRPC
	Checkout  CheckoutQueryRPC
}

type QueryTools struct {
	executor *Executor
	handlers map[string]HandlerFunc
}

func NewQueryTools(executor *Executor, clients QueryToolClients) *QueryTools {
	handlers := make(map[string]HandlerFunc)
	registerQueryHandlers(handlers, productQueryHandlers(clients.Product))
	registerQueryHandlers(handlers, inventoryQueryHandlers(clients.Inventory))
	registerQueryHandlers(handlers, orderQueryHandlers(clients.Order))
	registerQueryHandlers(handlers, cartQueryHandlers(clients.Cart))
	registerQueryHandlers(handlers, couponQueryHandlers(clients.Coupon))
	registerQueryHandlers(handlers, checkoutQueryHandlers(clients.Checkout))
	queryTools := &QueryTools{executor: executor, handlers: handlers}
	queryTools.bindEinoTools()
	return queryTools
}

func (q *QueryTools) Execute(ctx context.Context, req ExecuteRequest) domain.AgentEvent {
	return q.executor.Execute(ctx, req, q.handlers[req.ToolName])
}

func (q *QueryTools) Handler(name string) (HandlerFunc, bool) {
	handler, ok := q.handlers[name]
	return handler, ok
}

// 将查询工具绑定到 eino 标准工具接口
func (q *QueryTools) bindEinoTools() {
	if q.executor == nil || q.executor.registry == nil {
		return
	}
	for name := range q.handlers {
		base, err := q.executor.registry.Tool(name)
		if err != nil {
			continue
		}
		q.executor.registry.tools[name] = &queryInvokableTool{name: name, base: base, queryTools: q}
	}
}

// queryInvokableTool 是基于InvokableTool封装的查询业务工具
type queryInvokableTool struct {
	name       string
	base       einotool.InvokableTool
	queryTools *QueryTools
}

// 提供工具的元信息，llm 使用这些信息决定是否以及如何调用该工具
func (t *queryInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.base.Info(ctx)
}

func (t *queryInvokableTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	// 获取工具执行上下文
	execution, ok := ctx.Value(toolExecutionContextKey{}).(ToolExecutionContext)
	// 校验userid
	if !ok || execution.UserID == 0 {
		return "", ErrToolExecutionContext
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		parseErr := fmt.Errorf("%w: invalid JSON arguments: %v", ErrInvalidToolArguments, err)
		event := t.queryTools.executor.Reject(ctx, executeRequestFromContext(execution, t.name, nil), parseErr)
		return event.DataJSON, fmt.Errorf("%w: %s", ErrQueryToolExecution, event.Content)
	}
	// 工具执行
	event := t.queryTools.Execute(ctx, executeRequestFromContext(execution, t.name, args))
	if event.Status != toolStatusSuccess {
		return event.DataJSON, fmt.Errorf("%w: %s", ErrQueryToolExecution, event.Content)
	}
	return event.DataJSON, nil
}

func executeRequestFromContext(execution ToolExecutionContext, toolName string, arguments map[string]any) ExecuteRequest {
	return ExecuteRequest{
		UserID:         execution.UserID,
		ConversationID: execution.ConversationID,
		MessageID:      execution.MessageID,
		ClientIP:       execution.ClientIP,
		ToolName:       toolName,
		Arguments:      arguments,
	}
}

// 注册查询工具处理函数
func registerQueryHandlers(target, source map[string]HandlerFunc) {
	for name, handler := range source {
		target[name] = handler
	}
}

// 检查参数中是否包含必需的字符串参数
func requiredStringArgument(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok {
		return "", invalidArgument(name, "is required")
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", invalidArgument(name, "must be a non-empty string")
	}
	return strings.TrimSpace(text), nil
}

func optionalStringArgument(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", invalidArgument(name, "must be a string")
	}
	return strings.TrimSpace(text), nil
}

func optionalStringListArgument(args map[string]any, name string) ([]string, error) {
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
				return nil, invalidArgument(name, "must contain only non-empty strings")
			}
			result = append(result, strings.TrimSpace(text))
		}
		return result, nil
	default:
		return nil, invalidArgument(name, "must be a string or string array")
	}
}

func requiredInt64Argument(args map[string]any, name string) (int64, error) {
	value, ok := args[name]
	if !ok {
		return 0, invalidArgument(name, "is required")
	}
	parsed, err := integerValue(value)
	if err != nil {
		return 0, invalidArgument(name, err.Error())
	}
	return parsed, nil
}

func optionalInt64Argument(args map[string]any, name string, fallback int64) (int64, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback, nil
	}
	parsed, err := integerValue(value)
	if err != nil {
		return 0, invalidArgument(name, err.Error())
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

func positiveInt32(value int64, name string) (int32, error) {
	if value <= 0 || value > math.MaxInt32 {
		return 0, invalidArgument(name, "must be a positive 32-bit integer")
	}
	return int32(value), nil
}

func positiveUint32(value int64, name string) (uint32, error) {
	if value <= 0 || value > math.MaxUint32 {
		return 0, invalidArgument(name, "must be a positive 32-bit integer")
	}
	return uint32(value), nil
}

func authenticatedUserID32(userID uint64) (int32, error) {
	if userID == 0 || userID > math.MaxInt32 {
		return 0, fmt.Errorf("%w: authenticated user_id is invalid", ErrInvalidToolArguments)
	}
	return int32(userID), nil
}

// 解析分页参数
func queryPagination(args map[string]any) (page, pageSize int32, err error) {
	pageValue, err := optionalInt64Argument(args, "page", int64(defaultQueryPage))
	if err != nil {
		return 0, 0, err
	}
	pageSizeValue, err := optionalInt64Argument(args, "page_size", int64(defaultQueryPageSize))
	if err != nil {
		return 0, 0, err
	}
	page, err = positiveInt32(pageValue, "page")
	if err != nil {
		return 0, 0, err
	}
	pageSize, err = positiveInt32(pageSizeValue, "page_size")
	if err != nil {
		return 0, 0, err
	}
	if pageSize > maxQueryPageSize {
		pageSize = maxQueryPageSize
	}
	return page, pageSize, nil
}

func invalidArgument(name, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidToolArguments, name, reason)
}

// 验证 RPC 响应
func validateRPCResponse(operation string, response any, statusCode int64, statusMessage string) error {
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

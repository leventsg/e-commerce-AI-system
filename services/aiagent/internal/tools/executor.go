package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	toolStatusSuccess = "success"
	toolStatusFailed  = "failed"
)

var (
	ErrToolHandlerRequired = errors.New("ai tool handler required")
	ErrToolExecution       = errors.New("tool execution failed")
)

var sensitiveToolArgumentKeys = []string{"user_id", "token", "session_id", "auth"}

type ExecuteRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ClientIP       string
	RunID          string
	CheckpointID   string
	ToolName       string
	Arguments      map[string]any
}

type HandlerRequest struct {
	UserID    uint64
	ToolName  string
	Arguments map[string]any
	Metadata  domain.Metadata
}

type HandlerResult struct {
	Data    any
	Summary string
}

type HandlerFunc func(context.Context, HandlerRequest) (HandlerResult, error)

// 注册工具处理函数到destination中
func mergeHandlers(destination, source map[string]HandlerFunc) {
	for name, handler := range source {
		destination[name] = handler
	}
}

type ToolCallRecord struct {
	ConversationID string
	UserID         uint64
	ToolName       string
	Arguments      map[string]any
	Status         string
	ResultSummary  string
	ErrorMessage   string
	Latency        time.Duration
	ResultData     any
	ClientIP       string
	Metadata       domain.Metadata
}

type ToolCallRecorder interface {
	RecordToolCall(ctx context.Context, record ToolCallRecord) error
}

type Executor struct {
	registry *Registry
	recorder ToolCallRecorder
}

type ExecutorOption func(*Executor)

func WithToolCallRecorder(recorder ToolCallRecorder) ExecutorOption {
	return func(e *Executor) {
		e.recorder = recorder
	}
}

// 创建一个 Executor 实例，将其与工具注册表和recoder关联起来
func NewExecutor(registry *Registry, opts ...ExecutorOption) *Executor {
	executor := &Executor{registry: registry}
	for _, opt := range opts {
		opt(executor)
	}
	if registry != nil {
		registry.setExecutor(executor)
	}
	return executor
}

// Execute 执行指定的工具，并返回执行结果事件
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest, handler HandlerFunc) domain.AgentEvent {
	startedAt := time.Now()
	// 获取工具元数据
	metadata, err := e.registry.Metadata(req.ToolName)
	if err != nil {
		event := failedToolEvent(req, req.ToolName, "工具未注册，无法执行。", err)
		_ = e.record(ctx, req, domain.Metadata{}, map[string]any{}, toolStatusFailed, "", err.Error(), nil, time.Since(startedAt))
		return event
	}

	// 清理工具参数中的敏感信息
	args := argx.SanitizeMapKeys(req.Arguments, sensitiveToolArgumentKeys)
	if handler == nil {
		event := failedToolEvent(req, metadata.Name, "工具暂不可用，请稍后重试。", ErrToolHandlerRequired)
		_ = e.record(ctx, req, metadata, args, toolStatusFailed, "", ErrToolHandlerRequired.Error(), nil, time.Since(startedAt))
		return event
	}

	timeout := time.Duration(metadata.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		// 默认超时时间为3s
		timeout = time.Duration(defaultQueryTimeoutSeconds) * time.Second
	}
	handlerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 执行工具处理函数
	result, err := runHandlerWithTimeout(handlerCtx, handler, HandlerRequest{
		UserID:    req.UserID,
		ToolName:  metadata.Name,
		Arguments: args,
		Metadata:  metadata,
	})
	latency := time.Since(startedAt)
	if err != nil {
		content := "工具调用未完成，请稍后重试。"
		if errors.Is(err, context.DeadlineExceeded) {
			content = "工具调用超时，未完成操作，请稍后重试。"
		}
		event := failedToolEvent(req, metadata.Name, content, err)
		_ = e.record(ctx, req, metadata, args, toolStatusFailed, "", err.Error(), nil, latency)
		return event
	}

	// 构建工具结果事件
	dataJSON := marshalToolData(result.Data)
	event := domain.AgentEvent{
		Type:             domain.EventToolResult,
		ConversationID:   req.ConversationID,
		MessageID:        req.MessageID,
		Tool:             metadata.Name,
		Status:           toolStatusSuccess,
		DataJSON:         dataJSON,
		Content:          strings.TrimSpace(result.Summary),
		Done:             true,
		BusinessExecuted: metadata.WriteOperation,
	}
	if recordErr := e.record(ctx, req, metadata, args, toolStatusSuccess, event.Content, "", result.Data, latency); recordErr != nil && metadata.WriteOperation {
		event.Status = toolStatusFailed
		event.Content = "操作已完成，但审计记录失败，请联系支持。"
		event.DataJSON = marshalToolData(map[string]any{
			"business_executed": true,
			"audit_error":       recordErr.Error(),
			"result":            result.Data,
		})
	}
	return event
}

func (e *Executor) Reject(ctx context.Context, req ExecuteRequest, cause error) domain.AgentEvent {
	return e.Execute(ctx, req, func(context.Context, HandlerRequest) (HandlerResult, error) {
		return HandlerResult{}, cause
	})
}

type handlerResponse struct {
	result HandlerResult
	err    error
}

func runHandlerWithTimeout(ctx context.Context, handler HandlerFunc, req HandlerRequest) (HandlerResult, error) {
	done := make(chan handlerResponse, 1)
	go func() {
		result, err := handler(ctx, req)
		done <- handlerResponse{result: result, err: err}
	}()

	// 超时控制，一般优雅关闭吧，handler内部应该监听ctx.Done()，避免goroutine泄漏
	select {
	case <-ctx.Done():
		return HandlerResult{}, ctx.Err()
	case response := <-done:
		if response.err != nil {
			return HandlerResult{}, response.err
		}
		if err := ctx.Err(); err != nil {
			return HandlerResult{}, err
		}
		return response.result, nil
	}
}

func failedToolEvent(req ExecuteRequest, toolName, content string, cause error) domain.AgentEvent {
	return domain.AgentEvent{
		Type:           domain.EventToolResult,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Tool:           toolName,
		Status:         toolStatusFailed,
		DataJSON:       marshalToolData(map[string]any{"error": safeErrorMessage(cause)}),
		Content:        content,
		Done:           true,
	}
}

// 记录工具调用的相关信息
func (e *Executor) record(ctx context.Context, req ExecuteRequest, metadata domain.Metadata, args map[string]any, status, summary, errMsg string, resultData any, latency time.Duration) error {
	if e.recorder == nil {
		return nil
	}
	record := ToolCallRecord{
		ConversationID: req.ConversationID,
		UserID:         req.UserID,
		ToolName:       req.ToolName,
		Arguments:      argx.SanitizeMapKeys(args, sensitiveToolArgumentKeys),
		Status:         status,
		ResultSummary:  summary,
		ErrorMessage:   errMsg,
		Latency:        latency,
		ResultData:     resultData,
		ClientIP:       req.ClientIP,
		Metadata:       metadata,
	}
	if err := e.recorder.RecordToolCall(ctx, record); err != nil {
		logx.Errorw("record ai tool call failed", logx.Field("tool", req.ToolName), logx.Field("err", err))
		return err
	}
	return nil
}

func marshalToolData(data any) string {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		raw, _ = json.Marshal(map[string]any{
			"error": fmt.Sprintf("marshal tool data: %v", err),
		})
	}
	return string(raw)
}

func safeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

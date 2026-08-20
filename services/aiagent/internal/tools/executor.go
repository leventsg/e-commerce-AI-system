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
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
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

var sensitiveToolArgumentKeys = []string{"user_id", "token", "auth"}

// 注册工具处理函数到destination中
func mergeHandlers(destination, source map[string]core.HandlerFunc) {
	for name, handler := range source {
		destination[name] = handler
	}
}

type ToolCallRecord struct {
	ConversationID string
	ToolCallID     string
	UserID         uint64
	ToolName       string
	Arguments      map[string]any
	Status         string
	ErrorMessage   string
	Latency        time.Duration
	ResultData     any
	BusinessData   any
	ClientIP       string
	Metadata       domain.Metadata
}

type toolResultEnvelope struct {
	Status          string             `json:"status"`
	ToolName        string             `json:"tool_name"`
	AttemptCount    int                `json:"attempt_count"`
	RetryCount      int                `json:"retry_count"`
	Error           *toolErrorEnvelope `json:"error,omitempty"`
	BusinessOutcome string             `json:"business_outcome"`
	Result          any                `json:"result,omitempty"`
	AttemptTrail    []toolAttemptTrail `json:"attempt_trail,omitempty"`
}

type toolErrorEnvelope struct {
	Kind                  string `json:"kind"`
	Code                  string `json:"code"`
	Message               string `json:"message"`
	RetryableInCurrentRun bool   `json:"retryable_in_current_run"`
	RetryAfterMs          int64  `json:"retry_after_ms,omitempty"`
	TerminalForRun        bool   `json:"terminal_for_run,omitempty"`
}

type toolAttemptTrail struct {
	Attempt    int    `json:"attempt"`
	Status     string `json:"status"`
	ErrorKind  string `json:"error_kind,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	RPCStarted bool   `json:"rpc_started"`
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
func (e *Executor) Execute(ctx context.Context, req core.ExecuteRequest, handler core.HandlerFunc) domain.AgentEvent {
	startedAt := time.Now()
	var tool core.Tool
	metadata, err := e.registry.Metadata(req.ToolName)
	if err != nil {
		toolErr := permanentToolError("tool_not_found", "工具不存在，无法完成请求。", BusinessOutcomeNotExecuted, err)
		event := failedToolEvent(req, req.ToolName, toolErr.SafeMessage, toolErr, 1, 0, nil)
		_ = e.record(ctx, req, domain.Metadata{}, map[string]any{}, toolStatusFailed, err.Error(), event.DataJSON, nil, time.Since(startedAt))
		return event
	}
	if e.registry != nil {
		tool = e.registry.tools[req.ToolName]
	}

	logx.Infow("ai tool execution start",
		logx.Field("component", "tool_executor"),
		logx.Field("stage", "tool_start"),
		logx.Field("tool_call_id", req.ToolCallID),
		logx.Field("tool", metadata.Name),
		logx.Field("conversation_id", req.ConversationID),
		logx.Field("user_id", req.UserID),
		logx.Field("run_id", req.RunID),
		logx.Field("arguments", marshalToolData(req.Arguments)),
		logx.Field("rpc_service", metadata.RPCService),
		logx.Field("rpc_method", metadata.RPCMethod),
		logx.Field("timeout_ms", metadata.TimeoutSeconds*1000),
	)
	// 清理工具参数中的敏感信息
	args := argx.SanitizeMapKeys(req.Arguments, sensitiveToolArgumentKeys)
	if handler == nil {
		toolErr := classifyToolError(ErrToolHandlerRequired, metadata, ctx)
		event := failedToolEvent(req, metadata.Name, toolErr.SafeMessage, toolErr, 1, 0, nil)
		_ = e.record(ctx, req, metadata, args, toolStatusFailed, ErrToolHandlerRequired.Error(), event.DataJSON, nil, time.Since(startedAt))
		return event
	}

	timeout := time.Duration(metadata.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		// 默认超时时间为3s
		timeout = time.Duration(defaultQueryTimeoutSeconds) * time.Second
	}
	policy := defaultRetryPolicy(tool)
	// 工具执行轨迹记录
	trail := make([]toolAttemptTrail, 0, policy.MaxRetries+1)
	attemptCount := 0
	retryCount := 0
	var lastErr *ToolError
	var lastCause error
	for {
		attemptCount++
		attemptStartedAt := time.Now()
		handlerCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err := runHandlerWithTimeout(handlerCtx, handler, core.HandlerRequest{
			UserID:         req.UserID,
			ConversationID: req.ConversationID,
			ToolName:       metadata.Name,
			Arguments:      args,
			Metadata:       metadata,
		})
		cancel()
		attemptLatency := time.Since(attemptStartedAt)
		// 如果执行成功，记录成功事件并返回
		if err == nil {
			envelope := toolResultEnvelope{
				Status:          toolStatusSuccess,
				ToolName:        metadata.Name,
				AttemptCount:    attemptCount,
				RetryCount:      retryCount,
				BusinessOutcome: businessOutcomeForSuccess(metadata),
				Result:          result.Data,
				AttemptTrail: append(trail, toolAttemptTrail{
					Attempt:    attemptCount,
					Status:     toolStatusSuccess,
					LatencyMs:  attemptLatency.Milliseconds(),
					RPCStarted: true,
				}),
			}
			dataJSON := marshalToolData(envelope)
			event := domain.AgentEvent{
				Type:             domain.EventToolResult,
				ConversationID:   req.ConversationID,
				MessageID:        req.MessageID,
				Tool:             metadata.Name,
				Status:           toolStatusSuccess,
				DataJSON:         dataJSON,
				Content:          strings.TrimSpace(result.Summary),
				Done:             true,
				BusinessExecuted: envelope.BusinessOutcome == BusinessOutcomeExecuted,
			}
			logx.Infow("ai tool execution success",
				logx.Field("component", "tool_executor"),
				logx.Field("stage", "tool_success"),
				logx.Field("tool_call_id", req.ToolCallID),
				logx.Field("tool", metadata.Name),
				logx.Field("conversation_id", req.ConversationID),
				logx.Field("run_id", req.RunID),
				logx.Field("status", toolStatusSuccess),
				logx.Field("latency_ms", time.Since(startedAt).Milliseconds()),
				logx.Field("attempt_count", attemptCount),
				logx.Field("retry_count", retryCount),
				logx.Field("business_outcome", envelope.BusinessOutcome),
				logx.Field("result", marshalToolData(result.Data)),
			)
			if recordErr := e.record(ctx, req, metadata, args, toolStatusSuccess, "", envelope, result.Data, time.Since(startedAt)); recordErr != nil && metadata.WriteOperation {
				auditErr := permanentToolError("audit_failed", "操作已完成，但审计记录失败，请联系支持。", BusinessOutcomeExecuted, recordErr)
				event = failedToolEvent(req, metadata.Name, auditErr.SafeMessage, auditErr, attemptCount, retryCount, envelope.AttemptTrail)
				event.BusinessExecuted = true
			}
			return event
		}
		lastCause = err
		lastErr = classifyToolError(err, metadata, ctx)
		trail = append(trail, toolAttemptTrail{
			Attempt:    attemptCount,
			Status:     toolStatusFailed,
			ErrorKind:  string(lastErr.Kind),
			ErrorCode:  lastErr.Code,
			LatencyMs:  attemptLatency.Milliseconds(),
			RPCStarted: true,
		})
		// 如果错误是永久性错误，或者已经达到最大重试次数，则停止重试
		if lastErr.Kind != ToolErrorKindTransient || retryCount >= policy.MaxRetries {
			break
		}
		// 计算下次重试时间
		delay := retryDelay(policy, retryCount+1)
		if !canStartNextAttempt(ctx, startedAt, policy, delay+timeout) {
			break
		}
		logx.Infow("ai tool retry scheduled",
			logx.Field("component", "tool_executor"),
			logx.Field("stage", "retry_scheduled"),
			logx.Field("tool", metadata.Name),
			logx.Field("tool_call_id", req.ToolCallID),
			logx.Field("conversation_id", req.ConversationID),
			logx.Field("run_id", req.RunID),
			logx.Field("attempt", attemptCount),
			logx.Field("retry_count", retryCount+1),
			logx.Field("delay_ms", delay.Milliseconds()),
			logx.Field("error_kind", lastErr.Kind),
			logx.Field("error_code", lastErr.Code),
		)
		retryCount++
		// 等待重试时间
		if waitErr := waitRetryDelay(ctx, delay); waitErr != nil {
			lastCause = waitErr
			lastErr = classifyToolError(waitErr, metadata, ctx)
			break
		}
	}
	// 走到这儿的，都是重试失败的
	if lastErr == nil {
		lastErr = permanentToolError("tool_execution_failed", "工具调用失败，请检查请求后重试。", BusinessOutcomeNotExecuted, lastCause)
	}
	event := failedToolEvent(req, metadata.Name, lastErr.SafeMessage, lastErr, attemptCount, retryCount, trail)
	if lastErr.Kind == ToolErrorKindTransient && retryCount >= policy.MaxRetries {
		logx.Errorw("ai tool retry exhausted",
			logx.Field("component", "tool_executor"),
			logx.Field("stage", "tool_failure"),
			logx.Field("tool", metadata.Name),
			logx.Field("tool_call_id", req.ToolCallID),
			logx.Field("conversation_id", req.ConversationID),
			logx.Field("run_id", req.RunID),
			logx.Field("attempt_count", attemptCount),
			logx.Field("retry_count", retryCount),
			logx.Field("error_kind", lastErr.Kind),
			logx.Field("error_code", lastErr.Code),
			logx.Field("safe_message", lastErr.SafeMessage),
			logx.Field("latency_ms", time.Since(startedAt).Milliseconds()),
		)
	} else {
		logx.Errorw("ai tool permanent failure",
			logx.Field("component", "tool_executor"),
			logx.Field("stage", "tool_failure"),
			logx.Field("tool", metadata.Name),
			logx.Field("tool_call_id", req.ToolCallID),
			logx.Field("conversation_id", req.ConversationID),
			logx.Field("run_id", req.RunID),
			logx.Field("attempt_count", attemptCount),
			logx.Field("retry_count", retryCount),
			logx.Field("error_kind", lastErr.Kind),
			logx.Field("error_code", lastErr.Code),
			logx.Field("safe_message", lastErr.SafeMessage),
			logx.Field("latency_ms", time.Since(startedAt).Milliseconds()),
		)
	}
	_ = e.record(ctx, req, metadata, args, toolStatusFailed, safeErrorMessage(lastCause), event.DataJSON, nil, time.Since(startedAt))
	return event
}

func (e *Executor) Reject(ctx context.Context, req core.ExecuteRequest, cause error) domain.AgentEvent {
	return e.Execute(ctx, req, func(context.Context, core.HandlerRequest) (core.HandlerResult, error) {
		return core.HandlerResult{}, cause
	})
}

type handlerResponse struct {
	result core.HandlerResult
	err    error
}

func runHandlerWithTimeout(ctx context.Context, handler core.HandlerFunc, req core.HandlerRequest) (core.HandlerResult, error) {
	done := make(chan handlerResponse, 1)
	go func() {
		result, err := handler(ctx, req)
		done <- handlerResponse{result: result, err: err}
	}()

	// 超时控制，一般优雅关闭吧，handler内部应该监听ctx.Done()，避免goroutine泄漏
	select {
	case <-ctx.Done():
		return core.HandlerResult{}, ctx.Err()
	case response := <-done:
		if response.err != nil {
			return core.HandlerResult{}, response.err
		}
		if err := ctx.Err(); err != nil {
			return core.HandlerResult{}, err
		}
		return response.result, nil
	}
}

func failedToolEvent(req core.ExecuteRequest, toolName, content string, cause *ToolError, attemptCount, retryCount int, trail []toolAttemptTrail) domain.AgentEvent {
	envelope := toolResultEnvelope{
		Status:          toolStatusFailed,
		ToolName:        toolName,
		AttemptCount:    attemptCount,
		RetryCount:      retryCount,
		BusinessOutcome: BusinessOutcomeNotExecuted,
		AttemptTrail:    trail,
	}
	if cause != nil {
		envelope.BusinessOutcome = cause.BusinessOutcome
		envelope.Error = &toolErrorEnvelope{
			Kind:                  string(cause.Kind),
			Code:                  cause.Code,
			Message:               cause.SafeMessage,
			RetryableInCurrentRun: false,
			TerminalForRun:        true,
		}
		if cause.RetryAfter > 0 {
			envelope.Error.RetryAfterMs = cause.RetryAfter.Milliseconds()
		}
	}
	return domain.AgentEvent{
		Type:             domain.EventToolResult,
		ConversationID:   req.ConversationID,
		MessageID:        req.MessageID,
		Tool:             toolName,
		Status:           toolStatusFailed,
		DataJSON:         marshalToolData(envelope),
		Content:          content,
		Done:             true,
		BusinessExecuted: envelope.BusinessOutcome == BusinessOutcomeExecuted,
	}
}

func failedToolEventFromError(req core.ExecuteRequest, toolName, content string, cause error) domain.AgentEvent {
	toolErr := permanentToolError("tool_execution_failed", content, BusinessOutcomeNotExecuted, cause)
	return failedToolEvent(req, toolName, content, toolErr, 1, 0, nil)
}

func businessOutcomeForSuccess(metadata domain.Metadata) string {
	if metadata.WriteOperation {
		return BusinessOutcomeExecuted
	}
	return BusinessOutcomeNotExecuted
}

// 记录工具调用的相关信息
func (e *Executor) record(ctx context.Context, req core.ExecuteRequest, metadata domain.Metadata, args map[string]any, status, errMsg string, resultData any, businessData any, latency time.Duration) error {
	if e.recorder == nil {
		return nil
	}
	record := ToolCallRecord{
		ConversationID: req.ConversationID,
		ToolCallID:     req.ToolCallID,
		UserID:         req.UserID,
		ToolName:       req.ToolName,
		Arguments:      argx.SanitizeMapKeys(args, sensitiveToolArgumentKeys),
		Status:         status,
		ErrorMessage:   errMsg,
		Latency:        latency,
		ResultData:     resultData,
		BusinessData:   businessData,
		ClientIP:       req.ClientIP,
		Metadata:       metadata,
	}
	if err := e.recorder.RecordToolCall(ctx, record); err != nil {
		logx.Errorw("record ai tool call failed", logx.Field("component", "tool_executor"), logx.Field("stage", "record_tool_call"), logx.Field("tool", req.ToolName), logx.Field("tool_call_id", req.ToolCallID), logx.Field("conversation_id", req.ConversationID), logx.Field("run_id", req.RunID), logx.Field("err", err))
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

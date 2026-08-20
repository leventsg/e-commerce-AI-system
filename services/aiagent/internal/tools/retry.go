package tools

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/utils/bizerr"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ToolErrorKind string

const (
	ToolErrorKindTransient ToolErrorKind = "transient"
	ToolErrorKindPermanent ToolErrorKind = "permanent"
)

const (
	BusinessOutcomeNotExecuted = "not_executed"
	BusinessOutcomeExecuted    = "executed"
	BusinessOutcomeUnknown     = "unknown"
)

const (
	defaultQueryMaxRetries        = 2
	defaultRetryInitialDelay      = 150 * time.Millisecond
	defaultRetryMaxDelay          = 10 * time.Second
	defaultRetryBackoffMultiplier = 2
	defaultRetryMaxElapsed        = 30 * time.Second
)

type ToolError struct {
	Kind            ToolErrorKind
	Code            string
	SafeMessage     string
	BusinessOutcome string
	RetryAfter      time.Duration
	StatusCode      int64
	Cause           error
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.SafeMessage
}

func (e *ToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

/*
classifyToolError 区分永久性错误（Permanent）和临时性错误（Transient）

classifyToolError(err)
1. context.Canceled（父上下文取消） → 永久错误
2. 已是 ToolError → 直接返回
3. 业务错误（bizerr） → 永久错误
4. 参数错误 → 永久错误
5. RPC 状态错误 → 永久错误
6. 工具不存在/不可用 → 永久错误
7. context.Canceled → 永久错误
8. context.DeadlineExceeded → 临时错误
9. RPC 不可用 → 临时错误
10. gRPC 错误 → 根据状态码分类
11. 网络超时 → 临时错误
12. 默认 → 永久错误
*/
func classifyToolError(err error, metadata domain.Metadata, parentCtx context.Context) *ToolError {
	if err == nil {
		return nil
	}
	if parentCtx != nil && errors.Is(parentCtx.Err(), context.Canceled) {
		return permanentToolError("context_canceled", "请求已取消。", BusinessOutcomeNotExecuted, err)
	}
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	if code, message, ok := bizerr.Parse(err); ok {
		safe := strings.TrimSpace(message)
		if safe == "" {
			safe = "业务请求未通过，请检查后重试。"
		}
		return &ToolError{
			Kind:            ToolErrorKindPermanent,
			Code:            fmt.Sprintf("business_%d", code),
			SafeMessage:     safe,
			BusinessOutcome: BusinessOutcomeNotExecuted,
			StatusCode:      int64(code),
			Cause:           err,
		}
	}
	if errors.Is(err, helper.ErrInvalidToolArguments) {
		return permanentToolError("invalid_arguments", "工具参数不正确，请根据错误信息调整后重试。", BusinessOutcomeNotExecuted, err)
	}
	var rpcStatusErr *helper.RPCStatusError
	if errors.As(err, &rpcStatusErr) {
		safe := strings.TrimSpace(rpcStatusErr.StatusMsg)
		if safe == "" {
			safe = "业务请求未通过，请检查后重试。"
		}
		return &ToolError{
			Kind:            ToolErrorKindPermanent,
			Code:            fmt.Sprintf("rpc_status_%d", rpcStatusErr.StatusCode),
			SafeMessage:     safe,
			BusinessOutcome: BusinessOutcomeNotExecuted,
			StatusCode:      rpcStatusErr.StatusCode,
			Cause:           err,
		}
	}
	if errors.Is(err, ErrToolNotFound) {
		return permanentToolError("tool_not_found", "工具不存在，无法完成请求。", BusinessOutcomeNotExecuted, err)
	}
	if errors.Is(err, ErrToolHandlerRequired) {
		return permanentToolError("tool_unavailable", "工具暂不可用，请稍后重试。", BusinessOutcomeNotExecuted, err)
	}
	if errors.Is(err, context.Canceled) {
		return permanentToolError("context_canceled", "请求已取消。", BusinessOutcomeNotExecuted, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		outcome := BusinessOutcomeNotExecuted
		if metadata.WriteOperation {
			outcome = BusinessOutcomeUnknown
		}
		return transientToolError("deadline_exceeded", "工具调用超时，请稍后重试。", outcome, err)
	}
	if errors.Is(err, helper.ErrQueryRPCUnavailable) {
		return transientToolError("rpc_unavailable", "依赖服务暂时不可用，请稍后重试。", BusinessOutcomeNotExecuted, err)
	}
	if grpcCode := status.Code(err); grpcCode != codes.OK {
		switch grpcCode {
		case codes.Unavailable, codes.DeadlineExceeded:
			return transientToolError(strings.ToLower(grpcCode.String()), "依赖服务暂时不可用，请稍后重试。", BusinessOutcomeNotExecuted, err)
		case codes.ResourceExhausted:
			return permanentToolError("resource_exhausted", "依赖服务繁忙，请稍后重试。", BusinessOutcomeNotExecuted, err)
		case codes.Aborted:
			return permanentToolError("business_aborted", "业务请求未通过，请检查后重试。", BusinessOutcomeNotExecuted, err)
		case codes.PermissionDenied, codes.Unauthenticated:
			return permanentToolError("permission_denied", "当前用户无权执行该工具。", BusinessOutcomeNotExecuted, err)
		case codes.InvalidArgument, codes.NotFound, codes.Unimplemented:
			return permanentToolError(strings.ToLower(grpcCode.String()), "工具请求参数或目标资源不正确。", BusinessOutcomeNotExecuted, err)
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return transientToolError("network_timeout", "依赖服务网络超时，请稍后重试。", BusinessOutcomeNotExecuted, err)
	}
	return permanentToolError("tool_execution_failed", "工具调用失败，请检查请求后重试。", BusinessOutcomeNotExecuted, err)
}

func permanentToolError(code, message, outcome string, cause error) *ToolError {
	return &ToolError{Kind: ToolErrorKindPermanent, Code: code, SafeMessage: message, BusinessOutcome: outcome, Cause: cause}
}

func transientToolError(code, message, outcome string, cause error) *ToolError {
	return &ToolError{Kind: ToolErrorKindTransient, Code: code, SafeMessage: message, BusinessOutcome: outcome, Cause: cause}
}

// 默认重试策略：2次重试，初始延迟 150ms，最大延迟 10s，退避系数 2，总超时 30s。
func defaultRetryPolicy(tool core.Tool) core.RetryPolicy {
	policy := tool.RetryPolicy
	if policy.MaxRetries == 0 && policy.InitialDelay == 0 && policy.MaxDelay == 0 && policy.BackoffMultiplier == 0 && policy.MaxElapsed == 0 {
		if tool.Metadata.WriteOperation {
			return core.RetryPolicy{}
		}
		policy = core.RetryPolicy{
			MaxRetries:        defaultQueryMaxRetries,
			InitialDelay:      defaultRetryInitialDelay,
			MaxDelay:          defaultRetryMaxDelay,
			BackoffMultiplier: defaultRetryBackoffMultiplier,
			MaxElapsed:        defaultRetryMaxElapsed,
		}
	}
	if tool.Metadata.WriteOperation {
		policy.MaxRetries = 0
	}
	return normalizeRetryPolicy(policy)
}

func normalizeRetryPolicy(policy core.RetryPolicy) core.RetryPolicy {
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	if policy.MaxRetries == 0 {
		return policy
	}
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = defaultRetryInitialDelay
	}
	if policy.MaxDelay < policy.InitialDelay {
		policy.MaxDelay = policy.InitialDelay
	}
	if policy.BackoffMultiplier < 1 {
		policy.BackoffMultiplier = 1
	}
	if policy.MaxElapsed <= 0 {
		policy.MaxElapsed = defaultRetryMaxElapsed
	}
	return policy
}

// retryDelay 根据重试策略和当前重试次数计算下一次重试的延迟时间。
func retryDelay(policy core.RetryPolicy, retryCount int) time.Duration {
	if retryCount <= 0 {
		retryCount = 1
	}
	delay := float64(policy.InitialDelay)
	for i := 1; i < retryCount; i++ {
		delay *= policy.BackoffMultiplier
	}
	if max := float64(policy.MaxDelay); delay > max {
		delay = max
	}
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay) + 1))
}

func waitRetryDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// 确保下一次重试不会超过总超时限制。
func canStartNextAttempt(ctx context.Context, startedAt time.Time, policy core.RetryPolicy, delay time.Duration) bool {
	now := time.Now()
	if policy.MaxElapsed > 0 && now.Sub(startedAt)+delay >= policy.MaxElapsed {
		return false
	}
	if deadline, ok := ctx.Deadline(); ok && now.Add(delay).After(deadline) {
		return false
	}
	return true
}

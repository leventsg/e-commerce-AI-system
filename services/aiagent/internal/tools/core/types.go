package core

import (
	"context"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type ToolKind int

const (
	ToolKindBusiness ToolKind = iota
	ToolKindCapability
)

type ToolVisibility int

const (
	ToolVisibilityRoot ToolVisibility = iota
	ToolVisibilityAllAgents
	ToolVisibilitySubAgents
)

type ConfirmationSummaryFunc func(context.Context, ExecuteRequest) (string, error)

type ExecuteRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	ToolCallID     string
	ClientIP       string
	RunID          string
	CheckpointID   string
	ToolName       string
	Arguments      map[string]any
}

type HandlerRequest struct {
	UserID         uint64
	ConversationID string
	ToolName       string
	Arguments      map[string]any
	Metadata       domain.Metadata
}

type HandlerResult struct {
	Data    any
	Summary string
}

type HandlerFunc func(context.Context, HandlerRequest) (HandlerResult, error)

type RetryPolicy struct {
	MaxRetries        int
	InitialDelay      time.Duration // 初始重试延迟
	MaxDelay          time.Duration // 最大重试延迟
	BackoffMultiplier float64       // 退避系数
	MaxElapsed        time.Duration // 总超时时间
}

type Tool struct {
	Name                string
	Desc                string
	Params              map[string]*schema.ParameterInfo
	Kind                ToolKind       // 工具类型，业务工具或能力工具
	Visibility          ToolVisibility // 工具可见性，root智能体、所有智能体或子智能体
	Metadata            domain.Metadata
	RetryPolicy         RetryPolicy
	Handler             HandlerFunc
	ConfirmationSummary ConfirmationSummaryFunc
}

package tools

import (
	"context"
	"errors"
	"time"

	"github.com/leventsg/e-commerce-AI-system/common/utils/argx"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/confirmation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

var ErrConfirmationCreatorRequired = errors.New("confirmation creator required")

type ConfirmationCreator interface {
	// Create 创建一个新的确认请求，存储在数据库中
	Create(ctx context.Context, req confirmation.CreateRequest) (*domain.Confirmation, error)
}

type ConfirmationResumeTargetBinder interface {
	BindResumeTarget(ctx context.Context, req confirmation.ResumeTargetRequest) (*domain.Confirmation, error)
}

type ApprovalManager struct {
	registry *Registry
	executor *Executor
	creator  ConfirmationCreator
}

func NewApprovalManager(registry *Registry, executor *Executor, creator ConfirmationCreator) *ApprovalManager {
	return &ApprovalManager{registry: registry, executor: executor, creator: creator}
}

func (m *ApprovalManager) RequiresConfirmation(toolName string) bool {
	return m != nil && m.registry != nil && m.registry.RequiresConfirmation(toolName)
}

// 用于创建高风险操作的确认请求
func (m *ApprovalManager) RequestConfirmation(ctx context.Context, req ExecuteRequest) (event domain.AgentEvent) {
	startedAt := time.Now()
	// 前置检查
	if m == nil || m.registry == nil {
		return failedToolEvent(req, req.ToolName, "确认请求暂不可用，请稍后重试。", ErrConfirmationCreatorRequired)
	}
	metadata, err := m.registry.Metadata(req.ToolName)
	if err != nil || !m.registry.RequiresConfirmation(req.ToolName) {
		if err == nil {
			err = confirmation.ErrConfirmationToolNotAllowed
		}
		return failedToolEvent(req, req.ToolName, "该操作不能进入确认流程。", err)
	}
	if m.creator == nil {
		return failedToolEvent(req, metadata.Name, "确认请求暂不可用，请稍后重试。", ErrConfirmationCreatorRequired)
	}
	// 参数清理
	args := argx.SanitizeMapKeys(req.Arguments, sensitiveToolArgumentKeys)
	req.Arguments = args
	defer func() {
		// 记录执行结果
		if m.executor == nil {
			return
		}
		recordMetadata := metadata
		recordMetadata.WriteOperation = false
		status := event.Status
		errMessage := ""
		if event.Type == domain.EventConfirmationRequired {
			status = toolStatusSuccess
		} else if status == toolStatusFailed {
			errMessage = event.Content
		}
		_ = m.executor.record(ctx, req, recordMetadata, args, status, event.Content, errMessage, event.DataJSON, time.Since(startedAt))
	}()
	// 确认摘要
	summary, err := m.registry.ConfirmationSummary(ctx, req)
	if err != nil {
		return failedToolEvent(req, metadata.Name, "无法创建确认请求，请检查操作参数。", err)
	}
	// 创建确认记录，存储到数据库
	created, err := m.creator.Create(ctx, confirmation.CreateRequest{
		UserID:         req.UserID,
		ConversationID: req.ConversationID,
		ToolName:       metadata.Name,
		Arguments:      args,
		Summary:        summary,
		RunID:          req.RunID,
		CheckpointID:   req.CheckpointID,
	})
	if err != nil || created == nil {
		if err == nil {
			err = ErrConfirmationCreatorRequired
		}
		return failedToolEvent(req, metadata.Name, "确认请求创建失败，请稍后重试。", err)
	}
	payload := map[string]any{
		"type":              domain.EventConfirmationRequired,
		"confirmation_id":   created.ID,
		"action":            metadata.Name,
		"summary":           created.Summary,
		"expires_at":        created.ExpiresAt.Unix(),
		"arguments_summary": args,
	}
	// 构建确认请求事件
	return domain.AgentEvent{
		Type:           domain.EventConfirmationRequired,
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Tool:           metadata.Name,
		Status:         confirmation.StatusPending,
		DataJSON:       marshalToolData(payload),
		Content:        created.Summary,
		ConfirmationID: created.ID,
		Action:         metadata.Name,
		Summary:        created.Summary,
		ExpiresAt:      created.ExpiresAt.Unix(),
		Done:           true,
	}
}

func (m *ApprovalManager) BindResumeTarget(ctx context.Context, req confirmation.ResumeTargetRequest) error {
	if m == nil || m.creator == nil {
		return ErrConfirmationCreatorRequired
	}
	binder, ok := m.creator.(ConfirmationResumeTargetBinder)
	if !ok {
		return ErrConfirmationCreatorRequired
	}
	_, err := binder.BindResumeTarget(ctx, req)
	return err
}

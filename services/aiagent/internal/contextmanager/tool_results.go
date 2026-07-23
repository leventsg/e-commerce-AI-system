package contextmanager

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

const (
	defaultRecentToolMessageLimit = 50
	successStatus                 = "success"
)

var (
	ErrToolResultNotFound    = errors.New("tool result not found")
	ErrToolResultUnavailable = errors.New("tool result unavailable")
)

type ToolMessagesModel interface {
	FindRecentToolMessages(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aimessages.AiMessages, error)
	FindToolMessageByID(ctx context.Context, userID uint64, conversationID, messageID string) (*aimessages.AiMessages, error)
}

type ToolResultStore struct {
	messages ToolMessagesModel
}

func NewToolResultStore(messages ToolMessagesModel) *ToolResultStore {
	return &ToolResultStore{messages: messages}
}

func (s *ToolResultStore) FindLatestResult(ctx context.Context, userID uint64, conversationID string) (*domain.ToolResultEnvelope, error) {
	if s.messages == nil {
		return nil, ErrToolResultNotFound
	}
	rows, err := s.messages.FindRecentToolMessages(ctx, userID, conversationID, defaultRecentToolMessageLimit)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		// 尝试解析为ToolResultEnvelope，如果解析成功则返回
		envelope, err := parseToolResultEnvelope(row)
		if err == nil {
			return envelope, nil
		}
		if !errors.Is(err, ErrToolResultUnavailable) && !errors.Is(err, ErrToolResultNotFound) {
			return nil, err
		}
	}
	return nil, ErrToolResultNotFound
}

// FindRecentRefs 查最近的工具调用引用
func (s *ToolResultStore) FindRecentRefs(ctx context.Context, userID uint64, conversationID string, limit int) ([]domain.ToolCallRef, error) {
	if s.messages == nil {
		return nil, ErrToolResultNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.messages.FindRecentToolMessages(ctx, userID, conversationID, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.ToolCallRef, 0, len(rows))
	for _, row := range rows {
		ref, err := parseToolCallRef(row)
		if err != nil {
			if errors.Is(err, ErrToolResultUnavailable) || errors.Is(err, ErrToolResultNotFound) || errors.Is(err, tools.ErrUnsupportedToolProjection) || errors.Is(err, tools.ErrInvalidToolResultJSON) {
				continue
			}
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *ToolResultStore) FindResultByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*domain.ToolResultEnvelope, error) {
	if s.messages == nil {
		return nil, ErrToolResultNotFound
	}
	row, err := s.messages.FindToolMessageByID(ctx, userID, conversationID, toolCallID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrToolResultNotFound
		}
		return nil, err
	}
	return parseToolResultEnvelope(row)
}

type toolMessageMetadata struct {
	ToolCallID     string                     `json:"tool_call_id"`
	ToolName       string                     `json:"tool_name"`
	Status         string                     `json:"status"`
	ConfirmationID string                     `json:"confirmation_id,omitempty"`
	DataJSON       string                     `json:"data_json,omitempty"`
	ToolResult     *domain.ToolResultEnvelope `json:"tool_result,omitempty"`
}

func BuildToolResultMetadata(toolCallID, toolName, status, confirmationID, dataJSON, summary string) (string, error) {
	meta := toolMessageMetadata{
		ToolCallID:     toolCallID,
		ToolName:       toolName,
		Status:         status,
		ConfirmationID: confirmationID,
		DataJSON:       dataJSON,
	}
	if meta.Status == successStatus && meta.ToolName != "" && dataJSON != "" {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(dataJSON), &raw); err == nil {
			meta.ToolResult = &domain.ToolResultEnvelope{
				ToolCallID: meta.ToolCallID,
				ToolName:   meta.ToolName,
				Status:     meta.Status,
				Data:       raw,
				Summary:    summary,
			}
		}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal tool result metadata: %w", err)
	}
	return string(raw), nil
}

// parseToolResultEnvelope 解析工具调用结果并封装为ToolResultEnvelope结构
func parseToolResultEnvelope(row *aimessages.AiMessages) (*domain.ToolResultEnvelope, error) {
	// 解析工具消息元数据
	meta, err := parseToolMetadata(row)
	if err != nil {
		return nil, err
	}
	if meta.ToolResult == nil {
		return nil, ErrToolResultUnavailable
	}
	envelope := meta.ToolResult
	if envelope.Status != successStatus {
		return nil, ErrToolResultUnavailable
	}
	if !tools.SupportsToolResultProjection(envelope.ToolName) {
		return nil, ErrToolResultUnavailable
	}
	if len(envelope.Data) == 0 || !json.Valid(envelope.Data) {
		return nil, ErrToolResultUnavailable
	}
	return envelope, nil
}

func parseToolCallRef(row *aimessages.AiMessages) (domain.ToolCallRef, error) {
	meta, err := parseToolMetadata(row)
	if err != nil {
		return domain.ToolCallRef{}, err
	}
	if meta.Status != successStatus {
		return domain.ToolCallRef{}, ErrToolResultUnavailable
	}
	var dataJSON []byte
	if meta.ToolResult != nil {
		if len(meta.ToolResult.Data) == 0 || meta.ToolResult.Status != successStatus || !tools.SupportsToolResultProjection(meta.ToolResult.ToolName) {
			return domain.ToolCallRef{}, ErrToolResultUnavailable
		}
		dataJSON = meta.ToolResult.Data
	} else if meta.DataJSON != "" {
		dataJSON = []byte(meta.DataJSON)
	}
	if len(dataJSON) == 0 {
		return domain.ToolCallRef{}, ErrToolResultUnavailable
	}
	return tools.ProjectToolCallRef(meta.ToolName, meta.Status, row.Content, meta.ToolCallID, row.CreatedAt, dataJSON)
}

// parseToolMetadata 解析工具消息元数据
func parseToolMetadata(row *aimessages.AiMessages) (toolMessageMetadata, error) {
	if row == nil || row.Role != conversation.RoleTool || !row.Metadata.Valid || row.Metadata.String == "" {
		return toolMessageMetadata{}, ErrToolResultNotFound
	}
	var meta toolMessageMetadata
	if err := json.Unmarshal([]byte(row.Metadata.String), &meta); err != nil {
		return toolMessageMetadata{}, ErrToolResultUnavailable
	}
	return meta, nil
}

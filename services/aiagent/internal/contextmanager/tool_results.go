package contextmanager

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
)

const (
	defaultRecentToolMessageLimit = 50
)

var (
	ErrToolResultNotFound    = errors.New("tool result not found")
	ErrToolResultUnavailable = errors.New("tool result unavailable")
)

type ToolCallsModel interface {
	FindRecentSuccessfulToolCalls(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aitoolcalls.AiToolCalls, error)
	FindToolCallByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*aitoolcalls.AiToolCalls, error)
}

type ToolCallStore struct {
	toolCalls ToolCallsModel
}

func NewToolCallStore(toolCalls ToolCallsModel) *ToolCallStore {
	return &ToolCallStore{toolCalls: toolCalls}
}

func (s *ToolCallStore) FindLatestToolResult(ctx context.Context, userID uint64, conversationID string) (*aitoolcalls.AiToolCalls, error) {
	if s.toolCalls == nil {
		return nil, ErrToolResultNotFound
	}
	rows, err := s.toolCalls.FindRecentSuccessfulToolCalls(ctx, userID, conversationID, defaultRecentToolMessageLimit)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row != nil {
			return row, nil
		}
	}
	return nil, ErrToolResultNotFound
}

// FindRecentToolCall 查最近的工具调用。
func (s *ToolCallStore) FindRecentToolCall(ctx context.Context, userID uint64, conversationID string, limit int) ([]*aitoolcalls.AiToolCalls, error) {
	if s.toolCalls == nil {
		return nil, ErrToolResultNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.toolCalls.FindRecentSuccessfulToolCalls(ctx, userID, conversationID, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ToolCallStore) FindToolCallByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*aitoolcalls.AiToolCalls, error) {
	if s.toolCalls == nil {
		return nil, ErrToolResultNotFound
	}
	row, err := s.toolCalls.FindToolCallByCallID(ctx, userID, conversationID, toolCallID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrToolResultNotFound
		}
		if errors.Is(err, aitoolcalls.ErrNotFound) {
			return nil, ErrToolResultNotFound
		}
		return nil, err
	}
	return row, nil
}

type toolMessageMetadata struct {
	ToolCallID     string `json:"tool_call_id"`
	ToolName       string `json:"tool_name"`
	Status         string `json:"status"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	DataJSON       string `json:"data_json,omitempty"`
}

func BuildToolResultMetadata(toolCallID, toolName, status, confirmationID, dataJSON, _ string) (string, error) {
	meta := toolMessageMetadata{
		ToolCallID:     toolCallID,
		ToolName:       toolName,
		Status:         status,
		ConfirmationID: confirmationID,
		DataJSON:       dataJSON,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal tool result metadata: %w", err)
	}
	return string(raw), nil
}

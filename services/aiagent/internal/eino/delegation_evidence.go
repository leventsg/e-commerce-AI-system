package eino

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	aitoolcalls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/contextmanager"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

var ErrDelegationEvidenceUnavailable = errors.New("delegation evidence unavailable")

// ResolvedDelegationEvidence is a server-owned record. UserID and ConversationID
// are persisted for ownership checks; omit them when projecting model context.
type ResolvedDelegationEvidence struct {
	Ref            string          `json:"ref"`
	UserID         uint64          `json:"user_id"`
	ConversationID string          `json:"conversation_id"`
	ToolName       string          `json:"tool_name,omitempty"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	Status         string          `json:"status,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	Role           string          `json:"role,omitempty"`
	Content        string          `json:"content,omitempty"`
}

// DelegationEvidenceResolver uses scoped storage functions. Concrete callbacks
// reuse existing model methods without introducing a second storage interface.
type DelegationEvidenceResolver struct {
	FindToolCall func(context.Context, uint64, string, string) (*aitoolcalls.AiToolCalls, error)
	FindMessages func(context.Context, uint64, string, []string) ([]*aimessages.AiMessages, error)
}

// Resolve loads only evidence owned by the authenticated user and conversation.
// Missing, malformed and foreign references are intentionally indistinguishable.
func (r *DelegationEvidenceResolver) Resolve(ctx context.Context, ref string) (*ResolvedDelegationEvidence, error) {
	execution, ok := helper.ToolExecutionFromContext(ctx)
	kind, id, found := strings.Cut(ref, ":")
	if r == nil || !ok || execution.UserID == 0 || strings.TrimSpace(execution.ConversationID) == "" || !found || id == "" || strings.TrimSpace(id) != id || len(ref) > 2000 {
		return nil, ErrDelegationEvidenceUnavailable
	}
	result := &ResolvedDelegationEvidence{Ref: ref, UserID: execution.UserID, ConversationID: execution.ConversationID}
	switch kind {
	case "tool_call":
		if r.FindToolCall == nil {
			return nil, ErrDelegationEvidenceUnavailable
		}
		row, err := r.FindToolCall(ctx, execution.UserID, execution.ConversationID, id)
		if err != nil {
			return nil, delegationEvidenceStorageError(err)
		}
		if row == nil || row.UserId != execution.UserID || row.ConversationId != execution.ConversationID || row.ToolCallId != id {
			return nil, ErrDelegationEvidenceUnavailable
		}
		if (row.Arguments != "" && !json.Valid([]byte(row.Arguments))) || (row.Result != "" && !json.Valid([]byte(row.Result))) {
			return nil, ErrDelegationEvidenceUnavailable
		}
		result.ToolName, result.Status, result.ErrorMessage = row.ToolName, row.Status, row.ErrorMessage
		result.Arguments, result.Result = json.RawMessage(row.Arguments), json.RawMessage(row.Result)
	case "message":
		if r.FindMessages == nil {
			return nil, ErrDelegationEvidenceUnavailable
		}
		rows, err := r.FindMessages(ctx, execution.UserID, execution.ConversationID, []string{id})
		if err != nil {
			return nil, delegationEvidenceStorageError(err)
		}
		if len(rows) != 1 || rows[0] == nil {
			return nil, ErrDelegationEvidenceUnavailable
		}
		row := rows[0]
		if row.UserId != execution.UserID || row.ConversationId != execution.ConversationID || row.MsgId != id {
			return nil, ErrDelegationEvidenceUnavailable
		}
		result.Role, result.Content = row.Role, row.Content
	default:
		return nil, ErrDelegationEvidenceUnavailable
	}
	// Reject instead of silently truncating a critical instruction or tool result.
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 32*1024 {
		return nil, ErrDelegationEvidenceUnavailable
	}
	return result, nil
}

func delegationEvidenceStorageError(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, aimessages.ErrNotFound) || errors.Is(err, aitoolcalls.ErrNotFound) || errors.Is(err, contextmanager.ErrToolResultNotFound) {
		return ErrDelegationEvidenceUnavailable
	}
	return fmt.Errorf("resolve delegation evidence: %w", err)
}

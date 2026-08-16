package memory

import "context"

const (
	ConversationValueKeyUserID           = "userID"
	ConversationValueKeyConversationID   = "conversationID"
	ConversationValueKeyRunID            = "runID"
	ConversationValueKeyCurrentMessageID = "currentMessageID"
	ConversationValueKeyClientMessageID  = "clientMessageID"
)

type ConversationMetadata struct {
	UserID           uint64
	ConversationID   string
	RunID            string
	CurrentMessageID string
	ClientMessageID  string
}

type conversationMetadataContextKey struct{}

func NewConversationValues(meta ConversationMetadata) map[string]any {
	return map[string]any{
		ConversationValueKeyUserID:           meta.UserID,
		ConversationValueKeyConversationID:   meta.ConversationID,
		ConversationValueKeyRunID:            meta.RunID,
		ConversationValueKeyCurrentMessageID: meta.CurrentMessageID,
		ConversationValueKeyClientMessageID:  meta.ClientMessageID,
	}
}

func ConversationMetadataFromMap(values map[string]any) (ConversationMetadata, bool) {
	if values == nil {
		return ConversationMetadata{}, false
	}
	userID, ok := uint64Value(values[ConversationValueKeyUserID])
	if !ok || userID == 0 {
		return ConversationMetadata{}, false
	}
	conversationID, _ := values[ConversationValueKeyConversationID].(string)
	if conversationID == "" {
		return ConversationMetadata{}, false
	}
	runID, _ := values[ConversationValueKeyRunID].(string)
	currentMessageID, _ := values[ConversationValueKeyCurrentMessageID].(string)
	clientMessageID, _ := values[ConversationValueKeyClientMessageID].(string)
	return ConversationMetadata{
		UserID:           userID,
		ConversationID:   conversationID,
		RunID:            runID,
		CurrentMessageID: currentMessageID,
		ClientMessageID:  clientMessageID,
	}, true
}

func ContextWithConversationMetadata(ctx context.Context, meta ConversationMetadata) context.Context {
	return context.WithValue(ctx, conversationMetadataContextKey{}, meta)
}

func ConversationMetadataFromContext(ctx context.Context) (ConversationMetadata, bool) {
	meta, ok := ctx.Value(conversationMetadataContextKey{}).(ConversationMetadata)
	if !ok {
		return ConversationMetadata{}, false
	}
	return meta, meta.UserID != 0 && meta.ConversationID != ""
}

func uint64Value(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint32:
		return uint64(typed), true
	case uint:
		return uint64(typed), true
	case int:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint64(typed), true
	default:
		return 0, false
	}
}

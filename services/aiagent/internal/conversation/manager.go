package conversation

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	aimessages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
)

const (
	StatusActive = "active"

	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"

	defaultHistoryLimit = 20
)

var ErrConversationForbidden = errors.New("conversation does not belong to current user")

type conversationsModel interface {
	Insert(ctx context.Context, data *aiconversations.AiConversations) (sql.Result, error)
	FindOne(ctx context.Context, id string) (*aiconversations.AiConversations, error)
}

type messagesModel interface {
	Insert(ctx context.Context, data *aimessages.AiMessages) (sql.Result, error)
	FindRecentByConversationID(ctx context.Context, conversationID string, limit int) ([]*aimessages.AiMessages, error)
	FindUserMessageByClientMessageID(ctx context.Context, userID uint64, clientMessageID string) (*aimessages.AiMessages, error)
}

type Manager interface {
	// AI 对话预处理：会话初始化、消息存储和历史加载
	Prepare(ctx context.Context, req PrepareRequest) (*PreparedConversation, error)
}

type PrepareRequest struct {
	UserID          uint64
	ConversationID  string
	MessageID       string
	ClientMessageID string
	Content         string
	Metadata        sql.NullString
}

type PreparedConversation struct {
	ConversationID  string
	UserMessageID   string
	UserMessageSeq  uint64
	ClientMessageID string
	Duplicate       bool
}

type manager struct {
	conversations conversationsModel
	messages      messagesModel
	historyLimit  int
}

type Option func(*manager)

func WithHistoryLimit(limit int) Option {
	return func(m *manager) {
		if limit > 0 {
			m.historyLimit = limit
		}
	}
}

func NewManager(conversations conversationsModel, messages messagesModel, opts ...Option) Manager {
	m := &manager{
		conversations: conversations,
		messages:      messages,
		historyLimit:  defaultHistoryLimit,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// AI 对话预处理：会话初始化、消息存储
func (m *manager) Prepare(ctx context.Context, req PrepareRequest) (*PreparedConversation, error) {
	conversationID := req.ConversationID
	clientMessageID := req.ClientMessageID
	if clientMessageID != "" {
		existing, err := m.messages.FindUserMessageByClientMessageID(ctx, req.UserID, clientMessageID)
		// 请求已经处理过，则Duplicate设为true
		if err == nil && existing != nil {
			if conversationID != "" && existing.ConversationId != conversationID {
				return nil, ErrConversationForbidden
			}
			return &PreparedConversation{
				ConversationID:  existing.ConversationId,
				UserMessageID:   existing.MsgId,
				UserMessageSeq:  existing.Seq,
				ClientMessageID: clientMessageID,
				Duplicate:       true,
			}, nil
		}
		if err != nil && !errors.Is(err, aimessages.ErrNotFound) {
			return nil, err
		}
	}
	if conversationID == "" {
		conversationID = newID("conv")
		if _, err := m.conversations.Insert(ctx, &aiconversations.AiConversations{
			Id:     conversationID,
			UserId: req.UserID,
			Title:  "",
			Status: StatusActive,
		}); err != nil {
			return nil, err
		}
	} else {
		// 检查会话是否属于当前用户
		conversation, err := m.conversations.FindOne(ctx, conversationID)
		if err != nil {
			return nil, err
		}
		if conversation.UserId != req.UserID {
			return nil, ErrConversationForbidden
		}
	}

	messageID := req.MessageID
	if messageID == "" {
		messageID = newID("msg")
	}

	// 插入用户消息到数据库
	if _, err := m.messages.Insert(ctx, &aimessages.AiMessages{
		MsgId:           messageID,
		ConversationId:  conversationID,
		UserId:          req.UserID,
		Role:            RoleUser,
		Content:         req.Content,
		Metadata:        req.Metadata,
		ClientMessageId: sql.NullString{String: clientMessageID, Valid: clientMessageID != ""},
		CreatedAt:       time.Now(),
	}); err != nil {
		return nil, err
	}

	return &PreparedConversation{
		ConversationID:  conversationID,
		UserMessageID:   messageID,
		ClientMessageID: clientMessageID,
	}, nil
}

// 生成唯一 ID，带前缀
func newID(prefix string) string {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return prefix + "_" + id.String()
}

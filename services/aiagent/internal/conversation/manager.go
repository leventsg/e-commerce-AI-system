package conversation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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
}

type Manager interface {
	Prepare(ctx context.Context, req PrepareRequest) (*PreparedConversation, error)
}

type PrepareRequest struct {
	UserID         uint64
	ConversationID string
	MessageID      string
	Content        string
	Metadata       sql.NullString
}

type PreparedConversation struct {
	ConversationID string
	UserMessageID  string
	History        []*aimessages.AiMessages
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

// AI 对话预处理：会话初始化、消息存储和历史加载
func (m *manager) Prepare(ctx context.Context, req PrepareRequest) (*PreparedConversation, error) {
	conversationID := strings.TrimSpace(req.ConversationID)
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

	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		messageID = newID("msg")
	}

	// 插入用户消息到数据库
	if _, err := m.messages.Insert(ctx, &aimessages.AiMessages{
		Id:             messageID,
		ConversationId: conversationID,
		UserId:         req.UserID,
		Role:           RoleUser,
		Content:        req.Content,
		Metadata:       req.Metadata,
		CreatedAt:      time.Now(),
	}); err != nil {
		return nil, err
	}

	// 查询会话近期历史消息
	history, err := m.messages.FindRecentByConversationID(ctx, conversationID, m.historyLimit)
	if err != nil {
		return nil, err
	}
	return &PreparedConversation{
		ConversationID: conversationID,
		UserMessageID:  messageID,
		History:        history,
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

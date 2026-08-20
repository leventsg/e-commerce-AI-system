package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	aiconversations "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversations"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// ListMessagesLogic 返回当前用户指定会话的全部分页消息，包含 user/assistant/tool 三种角色。
type ListMessagesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMessagesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMessagesLogic {
	return &ListMessagesLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListMessagesLogic) ListMessages(in *aiagent.ListMessagesRequest) (*aiagent.ListMessagesResponse, error) {
	if in == nil || in.UserId == 0 {
		return nil, errors.New("用户身份无效")
	}
	conversationID := strings.TrimSpace(in.ConversationId)
	if conversationID == "" {
		return nil, errors.New("conversation_id 不能为空")
	}
	page, pageSize := normalizeHistoryPage(in.Page, in.PageSize)
	offset := (page - 1) * pageSize
	userID := uint64(in.UserId)

	// 会话归属校验：会话不存在或不属于当前用户时直接拒绝。
	conversation, err := l.svcCtx.ConversationsModel.FindOne(l.ctx, conversationID)
	if err != nil {
		if errors.Is(err, aiconversations.ErrNotFound) {
			return nil, errors.New("会话不存在")
		}
		l.Errorw("ai history find conversation failed",
			logx.Field("component", "list_messages_logic"),
			logx.Field("stage", "ownership_check"),
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err),
		)
		return nil, errors.New("历史消息加载失败，请稍后重试")
	}
	if conversation.UserId != userID {
		return nil, errors.New("无权访问该会话")
	}

	total, err := l.svcCtx.MessagesModel.CountByUserAndConversation(l.ctx, userID, conversationID)
	if err != nil {
		l.Errorw("ai history count messages failed",
			logx.Field("component", "list_messages_logic"),
			logx.Field("stage", "count"),
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err),
		)
		return nil, errors.New("历史消息加载失败，请稍后重试")
	}
	rows, err := l.svcCtx.MessagesModel.FindByUserAndConversation(l.ctx, userID, conversationID, pageSize, offset)
	if err != nil {
		l.Errorw("ai history list messages failed",
			logx.Field("component", "list_messages_logic"),
			logx.Field("stage", "query"),
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
			logx.Field("page", page),
			logx.Field("page_size", pageSize),
			logx.Field("err", err),
		)
		return nil, errors.New("历史消息加载失败，请稍后重试")
	}

	messages := make([]*aiagent.HistoryMessage, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		messages = append(messages, &aiagent.HistoryMessage{
			MessageId:       row.MsgId,
			Role:            row.Role,
			Content:         row.Content,
			MetadataJson:    row.Metadata.String,
			ClientMessageId: row.ClientMessageId.String,
			CreatedAt:       row.CreatedAt.Format(time.RFC3339),
		})
	}
	return &aiagent.ListMessagesResponse{
		ConversationId: conversationID,
		Total:          total,
		Messages:       messages,
	}, nil
}

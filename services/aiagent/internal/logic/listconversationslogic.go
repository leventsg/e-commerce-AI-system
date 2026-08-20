package logic

import (
	"context"
	"errors"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// ListConversationsLogic 返回当前用户的全部分页会话列表。
type ListConversationsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListConversationsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListConversationsLogic {
	return &ListConversationsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListConversationsLogic) ListConversations(in *aiagent.ListConversationsRequest) (*aiagent.ListConversationsResponse, error) {
	if in == nil || in.UserId == 0 {
		return nil, errors.New("用户身份无效")
	}
	page, pageSize := normalizeHistoryPage(in.Page, in.PageSize)
	offset := (page - 1) * pageSize
	userID := uint64(in.UserId)

	total, err := l.svcCtx.ConversationsModel.CountByUser(l.ctx, userID)
	if err != nil {
		l.Errorw("ai history list conversations count failed",
			logx.Field("component", "list_conversations_logic"),
			logx.Field("stage", "count"),
			logx.Field("user_id", userID),
			logx.Field("err", err),
		)
		return nil, errors.New("历史会话加载失败，请稍后重试")
	}
	items, err := l.svcCtx.ConversationsModel.FindByUserWithStats(l.ctx, userID, pageSize, offset)
	if err != nil {
		l.Errorw("ai history list conversations failed",
			logx.Field("component", "list_conversations_logic"),
			logx.Field("stage", "query"),
			logx.Field("user_id", userID),
			logx.Field("page", page),
			logx.Field("page_size", pageSize),
			logx.Field("err", err),
		)
		return nil, errors.New("历史会话加载失败，请稍后重试")
	}

	conversations := make([]*aiagent.ConversationSummary, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		conversations = append(conversations, &aiagent.ConversationSummary{
			ConversationId:     item.Id,
			Title:              item.Title,
			LastMessagePreview: item.LastMessagePreview,
			UpdatedAt:          historyTime(item.LastMessageAt, item.CreatedAt),
			MessageCount:       item.MessageCount,
		})
	}
	return &aiagent.ListConversationsResponse{
		Total:         total,
		Conversations: conversations,
	}, nil
}

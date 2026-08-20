package logic

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/svc"
	"github.com/leventsg/e-commerce-AI-system/apis/ai/internal/types"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/consts/code"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/aiagent"
	"github.com/zeromicro/go-zero/core/logx"
	xerrors "github.com/zeromicro/x/errors"
)

// HistoryLogic 历史会话与历史消息查询逻辑，用户 ID 只来自登录态上下文。
type HistoryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HistoryLogic {
	return &HistoryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HistoryLogic) ListSessions(req *types.ListSessionsRequest) (*types.ListSessionsResponse, error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok || userID == 0 || l.svcCtx == nil || l.svcCtx.AiAgentRpc == nil {
		return nil, xerrors.New(code.Fail, "unauthorized")
	}
	resp, err := l.svcCtx.AiAgentRpc.ListConversations(l.ctx, &aiagent.ListConversationsRequest{
		UserId:   userID,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		l.Errorw("call rpc ListConversations failed",
			logx.Field("component", "ai_gateway"),
			logx.Field("stage", "rpc_call"),
			logx.Field("user_id", userID),
			logx.Field("err", err),
		)
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if resp == nil {
		l.Errorw("rpc ListConversations returned nil response", logx.Field("user_id", userID))
		return nil, xerrors.New(code.ServerError, "RPC response is nil")
	}

	conversations := make([]types.SessionSummary, 0, len(resp.Conversations))
	for _, item := range resp.Conversations {
		if item == nil {
			continue
		}
		conversations = append(conversations, types.SessionSummary{
			ConversationID:     item.ConversationId,
			Title:              item.Title,
			LastMessagePreview: item.LastMessagePreview,
			UpdatedAt:          item.UpdatedAt,
			MessageCount:       item.MessageCount,
		})
	}
	return &types.ListSessionsResponse{
		Total:         resp.Total,
		Conversations: conversations,
	}, nil
}

func (l *HistoryLogic) ListMessages(req *types.ListMessagesRequest) (*types.ListMessagesResponse, error) {
	userID, ok := l.ctx.Value(biz.UserIDKey).(uint32)
	if !ok || userID == 0 || l.svcCtx == nil || l.svcCtx.AiAgentRpc == nil {
		return nil, xerrors.New(code.Fail, "unauthorized")
	}
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		return nil, xerrors.New(code.Fail, "conversation_id 不能为空")
	}
	resp, err := l.svcCtx.AiAgentRpc.ListMessages(l.ctx, &aiagent.ListMessagesRequest{
		UserId:         userID,
		ConversationId: conversationID,
		Page:           req.Page,
		PageSize:       req.PageSize,
	})
	if err != nil {
		l.Errorw("call rpc ListMessages failed",
			logx.Field("component", "ai_gateway"),
			logx.Field("stage", "rpc_call"),
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
			logx.Field("err", err),
		)
		return nil, xerrors.New(code.ServerError, code.ServerErrorMsg)
	}
	if resp == nil {
		l.Errorw("rpc ListMessages returned nil response",
			logx.Field("conversation_id", conversationID),
			logx.Field("user_id", userID),
		)
		return nil, xerrors.New(code.ServerError, "RPC response is nil")
	}

	messages := make([]types.HistoryMessage, 0, len(resp.Messages))
	for _, item := range resp.Messages {
		if item == nil {
			continue
		}
		message := types.HistoryMessage{
			MessageID:       item.MessageId,
			Role:            item.Role,
			Content:         item.Content,
			ClientMessageID: item.ClientMessageId,
			CreatedAt:       item.CreatedAt,
		}
		if raw := strings.TrimSpace(item.MetadataJson); raw != "" && json.Valid([]byte(raw)) {
			message.Metadata = json.RawMessage(raw)
		}
		messages = append(messages, message)
	}
	return &types.ListMessagesResponse{
		ConversationID: resp.ConversationId,
		Total:          resp.Total,
		Messages:       messages,
	}, nil
}

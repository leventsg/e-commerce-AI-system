package contextmanager

import (
	"context"
	"database/sql"
	"encoding/json"

	aiconversationsummaries "github.com/leventsg/e-commerce-AI-system/dal/model/ai/conversation_summaries"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

type summaryModel interface {
	FindLatestByUserConversation(ctx context.Context, userID uint64, conversationID string) (*aiconversationsummaries.AiConversationSummaries, error)
	Insert(ctx context.Context, data *aiconversationsummaries.AiConversationSummaries) (sql.Result, error)
}

type SummaryModelStore struct {
	model summaryModel
}

func NewSummaryStore(model summaryModel) *SummaryModelStore {
	return &SummaryModelStore{model: model}
}

func (s *SummaryModelStore) FindLatest(ctx context.Context, userID uint64, conversationID string) (*domain.ConversationSummary, error) {
	if s == nil || s.model == nil {
		return nil, ErrContextManagerUnavailable
	}
	row, err := s.model.FindLatestByUserConversation(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return conversationSummaryFromRow(row)
}

func (s *SummaryModelStore) Save(ctx context.Context, userID uint64, conversationID string, summary *domain.ConversationSummary) error {
	if s == nil || s.model == nil {
		return ErrContextManagerUnavailable
	}
	row, err := conversationSummaryToRow(userID, conversationID, summary)
	if err != nil {
		return err
	}
	_, err = s.model.Insert(ctx, row)
	return err
}

func conversationSummaryFromRow(row *aiconversationsummaries.AiConversationSummaries) (*domain.ConversationSummary, error) {
	var facts map[string]any
	if err := json.Unmarshal([]byte(row.KeyFacts), &facts); err != nil {
		return nil, err
	}
	var tasks []string
	if err := json.Unmarshal([]byte(row.OpenTasks), &tasks); err != nil {
		return nil, err
	}
	return &domain.ConversationSummary{
		Summary:               row.Summary,
		KeyFacts:              facts,
		OpenTasks:             tasks,
		CoveredUntilMessageID: row.CoveredUntilMessageId,
		CoveredUntilCreatedAt: row.CoveredUntilCreatedAt,
		TokenCount:            int(row.TokenCount),
	}, nil
}

func conversationSummaryToRow(userID uint64, conversationID string, summary *domain.ConversationSummary) (*aiconversationsummaries.AiConversationSummaries, error) {
	facts, err := json.Marshal(summary.KeyFacts)
	if err != nil {
		return nil, err
	}
	tasks, err := json.Marshal(summary.OpenTasks)
	if err != nil {
		return nil, err
	}
	return &aiconversationsummaries.AiConversationSummaries{
		Id:                    newSummaryID(),
		UserId:                userID,
		ConversationId:        conversationID,
		CoveredUntilCreatedAt: summary.CoveredUntilCreatedAt,
		CoveredUntilMessageId: summary.CoveredUntilMessageID,
		Summary:               summary.Summary,
		KeyFacts:              string(facts),
		OpenTasks:             string(tasks),
		TokenCount:            uint64(summary.TokenCount),
	}, nil
}

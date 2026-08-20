package capability_tools

import (
	"context"
	"errors"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	aimemory "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/memory"
	toolprompts "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/tools"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
)

var errToolHandlerRequired = errors.New("ai tool handler required")

type searchUserMemoryParams struct {
	Keywords []string `json:"keywords"`
	Match    string   `json:"match"`
	Type     string   `json:"type"`
	Since    string   `json:"since"`
	Until    string   `json:"until"`
	Limit    int      `json:"limit"`
	UserID   uint64   `json:"user_id"`
}

type searchUserMemoryResult struct {
	Total  int              `json:"total"`
	Events []aimemory.Event `json:"events"`
}

func newSearchUserMemoryTool(searcher aimemory.UserMemoryEventSearcher) core.Tool {
	return core.Tool{
		Name:       domain.ToolSearchUserMemory,
		Desc:       toolprompts.SearchUserMemoryDesc,
		Params:     toolprompts.SearchUserMemoryParameters,
		Kind:       core.ToolKindCapability,
		Visibility: core.ToolVisibilityRoot,
		Metadata: domain.Metadata{
			Name:           domain.ToolSearchUserMemory,
			Risk:           domain.RiskLow,
			TimeoutSeconds: 3,
		},
		Handler: searchUserMemoryHandler(searcher),
	}
}

func searchUserMemoryHandler(searcher aimemory.UserMemoryEventSearcher) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		if searcher == nil {
			return core.HandlerResult{}, errToolHandlerRequired
		}
		var params searchUserMemoryParams
		params.Keywords = stringSliceArgument(req.Arguments, "keywords")
		params.Match, _ = req.Arguments["match"].(string)
		params.Type, _ = req.Arguments["type"].(string)
		params.Since, _ = req.Arguments["since"].(string)
		params.Until, _ = req.Arguments["until"].(string)
		params.Limit = intNumberArgument(req.Arguments, "limit")
		limit := params.Limit
		if limit <= 0 {
			limit = 10
		}
		if limit > 30 {
			limit = 30
		}
		events, err := searcher.SearchUserMemoryEvents(ctx, aimemory.UserMemoryEventQuery{
			UserID:   req.UserID,
			Keywords: params.Keywords,
			Match:    strings.TrimSpace(params.Match),
			Type:     strings.TrimSpace(params.Type),
			Since:    strings.TrimSpace(params.Since),
			Until:    strings.TrimSpace(params.Until),
			Limit:    limit,
		})
		if err != nil {
			return core.HandlerResult{}, err
		}
		result := searchUserMemoryResult{Total: len(events), Events: events}
		return core.HandlerResult{Data: result, Summary: "已读取用户长期事件。"}, nil
	}
}

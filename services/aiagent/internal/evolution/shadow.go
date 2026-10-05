package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

// ShadowResult 是单个生产会话的影子执行对比：用生产轨迹重建的数据快照重放
// 用户输入，在模拟环境执行 Agent，写操作不真发业务 RPC，只记录"影子会做什么
// 动作"，再与原轨迹对比。除安全/一致性外，还对比 outcome 与成本以判断"是否有效"。
type ShadowResult struct {
	TraceID            string   `json:"trace_id"`
	SessionID          string   `json:"session_id,omitempty"`
	Agent              string   `json:"agent,omitempty"`
	OriginalTools      []string `json:"original_tools"`
	ShadowTools        []string `json:"shadow_tools"`
	OriginalWrites     []string `json:"original_writes"`
	ShadowWrites       []string `json:"shadow_writes"`
	ToolsMatch         bool     `json:"tools_match"`
	ExtraShadowWrites  []string `json:"extra_shadow_writes,omitempty"`  // 影子多做、原轨迹没有的写操作（副作用风险）
	MissingShadowWrites []string `json:"missing_shadow_writes,omitempty"` // 原轨迹有、影子未做的写操作
	ShadowReply        string   `json:"shadow_reply,omitempty"`
	OriginalReply      string   `json:"original_reply,omitempty"`
	// 有效性对比：任务结果、成本与澄清行为。
	OriginalOutcome    string `json:"original_outcome,omitempty"` // success | failed | unknown
	ShadowOutcome      string `json:"shadow_outcome,omitempty"`   // success | failed | unknown
	OutcomeImproved    bool   `json:"outcome_improved"`           // 原失败/未知 → 影子成功
	OriginalToolCount  int    `json:"original_tool_count"`
	ShadowToolCount    int    `json:"shadow_tool_count"`
	ToolCountReduced   bool   `json:"tool_count_reduced"`         // 结果未变差但工具调用更少
	AsksClarification  bool   `json:"asks_clarification"`         // 影子是否提出澄清（启发式）
	Error              string `json:"error,omitempty"`
}

// ShadowSummary 汇总一批影子执行结果，输出一致性、副作用风险与有效性信号。
type ShadowSummary struct {
	Total           int            `json:"total"`
	Consistent      int            `json:"consistent"`
	ToolMismatch    int            `json:"tool_mismatch"`
	SideEffectRisk  int            `json:"side_effect_risk"`  // 出现额外写操作的会话数
	OutcomeImproved int            `json:"outcome_improved"`  // 结果改善（原失败/未知→影子成功）的会话数
	ToolReduced     int            `json:"tool_reduced"`      // 结果未变差但工具更少的会话数
	Results         []ShadowResult `json:"results"`
}

// RunShadow 对一批生产会话做影子执行。skillSet 可为空（用内嵌 skill）或传入
// 候选 SkillSet（验证候选 skill 在真实数据快照上的影子行为）。
func RunShadow(ctx context.Context, cfg ABConfig, sessions []SessionEvidence, skillSet eino.SkillSet) (ShadowSummary, error) {
	if cfg.TestedFactory == nil {
		return ShadowSummary{}, fmt.Errorf("tested model factory is required")
	}
	summary := ShadowSummary{Results: []ShadowResult{}}
	for _, session := range sessions {
		result := shadowOne(ctx, cfg, session, skillSet)
		summary.Total++
		if result.Error != "" {
			continue
		}
		if result.ToolsMatch {
			summary.Consistent++
		} else {
			summary.ToolMismatch++
		}
		if len(result.ExtraShadowWrites) > 0 {
			summary.SideEffectRisk++
		}
		if result.OutcomeImproved {
			summary.OutcomeImproved++
		}
		if result.ToolCountReduced {
			summary.ToolReduced++
		}
		summary.Results = append(summary.Results, result)
	}
	return summary, nil
}

func shadowOne(ctx context.Context, cfg ABConfig, session SessionEvidence, skillSet eino.SkillSet) ShadowResult {
	result := ShadowResult{
		TraceID:       session.TraceID,
		SessionID:     session.SessionID,
		Agent:         session.Agent,
		OriginalReply: session.Output,
	}
	originalTools, originalWrites := collectTraceTools(session.ToolCalls)
	result.OriginalTools = originalTools
	result.OriginalWrites = originalWrites

	if session.Missing || session.Truncated || session.Input == "" {
		result.Error = "missing or truncated evidence"
		return result
	}

	fixture, err := buildFixtureFromTrace(session)
	if err != nil {
		result.Error = "fixture rebuild failed: " + err.Error()
		return result
	}

	req := eino.EvaluationRequest{Op: "chat", TrialID: "shadow_" + session.TraceID, Content: session.Input, Fixture: fixture}
	trial, err := eino.NewEvaluationTrial(ctx, req, cfg.TestedConfig, cfg.TestedFactory, nil, eino.WithSkillSet(skillSet))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	response := trial.Execute(ctx, req)
	if response.Error != "" {
		result.Error = response.Error
		return result
	}

	shadowTools := []string{}
	shadowWrites := []string{}
	for _, tr := range response.Trajectory {
		if tr.Kind != "tool" {
			continue
		}
		shadowTools = append(shadowTools, tr.Tool)
		if writeTools[tr.Tool] {
			shadowWrites = append(shadowWrites, tr.Tool)
		}
	}
	result.ShadowTools = shadowTools
	result.ShadowWrites = shadowWrites
	result.ShadowReply = response.AssistantText
	result.ToolsMatch = sameToolSet(originalTools, shadowTools)
	result.ExtraShadowWrites = setDifference(shadowWrites, originalWrites)
	result.MissingShadowWrites = setDifference(originalWrites, shadowWrites)
	// 有效性对比：outcome / 工具次数 / 澄清。
	result.OriginalOutcome = inferTraceOutcome(session.ToolCalls)
	result.ShadowOutcome = inferShadowOutcome(response)
	result.OriginalToolCount = len(originalTools)
	result.ShadowToolCount = len(shadowTools)
	result.OutcomeImproved = (result.OriginalOutcome == "failed" || result.OriginalOutcome == "unknown") && result.ShadowOutcome == "success"
	result.ToolCountReduced = result.ShadowOutcome != "failed" && result.OriginalOutcome != "failed" && result.ShadowToolCount < result.OriginalToolCount
	result.AsksClarification = asksClarification(response.AssistantText)
	return result
}

// inferTraceOutcome 从原轨迹工具调用推断任务结果：优先取最后一个写工具的
// business_outcome，否则按读工具是否有失败/unknown 判断。
func inferTraceOutcome(calls []ToolEvidence) string {
	for i := len(calls) - 1; i >= 0; i-- {
		if writeTools[calls[i].Tool] {
			return normalizeOutcome(calls[i].Outcome, calls[i].Status)
		}
	}
	for _, c := range calls {
		if c.Status == "failed" {
			return "failed"
		}
	}
	if len(calls) == 0 {
		return "unknown"
	}
	return "success"
}

// normalizeOutcome 把 business_outcome 或工具 status 归一为 success/failed/unknown。
func normalizeOutcome(outcome, status string) string {
	switch outcome {
	case "executed":
		return "success"
	case "not_executed":
		return "failed"
	case "unknown":
		return "unknown"
	}
	switch status {
	case "success":
		return "success"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

// inferShadowOutcome 从影子执行响应推断结果：有 error 或写工具失败则 failed。
func inferShadowOutcome(response eino.EvaluationResponse) string {
	if response.Error != "" {
		return "failed"
	}
	for _, tr := range response.Trajectory {
		if tr.Kind == "tool" && writeTools[tr.Tool] && tr.Status == "failed" {
			return "failed"
		}
	}
	return "success"
}

// asksClarification 启发式判断最终回复是否在向用户澄清（不精确，仅作行为信号）。
func asksClarification(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "？") && !strings.Contains(text, "?") {
		return false
	}
	for _, kw := range []string{"请问", "是否", "您想", "哪种", "哪一个", "还是", "确认", "要不要"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func collectTraceTools(calls []ToolEvidence) ([]string, []string) {
	tools := []string{}
	writes := []string{}
	for _, call := range calls {
		tools = append(tools, call.Tool)
		if writeTools[call.Tool] {
			writes = append(writes, call.Tool)
		}
	}
	return tools, writes
}

// buildFixtureFromTrace 从生产轨迹的读操作结果重建 evaluation.State 数据快照，
// 使影子执行尽量贴近生产真实数据。写操作结果不重建（写操作是"已发生的动作"，
// 由对比阶段判断影子是否会做同样动作）。
func buildFixtureFromTrace(session SessionEvidence) (json.RawMessage, error) {
	state := map[string]any{
		"products":  map[string]any{},
		"inventory": map[string]any{},
		"cart":      map[string]any{},
		"coupons":   map[string]any{},
		"orders":    map[string]any{},
		"addresses": map[string]any{},
		"checkouts": map[string]any{},
	}
	for _, call := range session.ToolCalls {
		var result map[string]any
		if call.Result == "" || json.Unmarshal([]byte(call.Result), &result) != nil {
			continue
		}
		switch call.Tool {
		case "product_search", "product_recommend":
			mergeByID(state["products"].(map[string]any), result["products"], "product_id")
		case "product_detail":
			mergeByID(state["products"].(map[string]any), result["product"], "product_id")
		case "inventory_get":
			if v, ok := result["inventory"]; ok {
				state["inventory"].(map[string]any)[fmt.Sprint(result["product_id"])] = v
			}
		case "cart_list":
			mergeByID(state["cart"].(map[string]any), result["items"], "cart_item_id")
		case "order_get":
			mergeByID(state["orders"].(map[string]any), result["order"], "order_id")
		case "order_list":
			mergeByID(state["orders"].(map[string]any), result["orders"], "order_id")
		case "coupon_list", "coupon_my_list":
			mergeByID(state["coupons"].(map[string]any), result["coupons"], "coupon_id")
		case "address_list":
			mergeByID(state["addresses"].(map[string]any), result["addresses"], "address_id")
		case "checkout_detail":
			mergeByID(state["checkouts"].(map[string]any), result, "pre_order_id")
		}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// mergeByID 把一组对象按指定 id 字段合并进目标 map。
func mergeByID(dst map[string]any, src any, idField string) {
	switch v := src.(type) {
	case map[string]any:
		mergeOne(dst, v, idField)
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				mergeOne(dst, m, idField)
			}
		}
	}
}

func mergeOne(dst map[string]any, item map[string]any, idField string) {
	id := fmt.Sprint(item[idField])
	if id == "" || id == "<nil>" {
		return
	}
	dst[id] = item
}

// sameToolSet 判断两个工具名序列是否在集合意义上一致（忽略顺序与重复）。
func sameToolSet(a, b []string) bool {
	return sameSet(counted(a), counted(b))
}

func setDifference(a, b []string) []string {
	seen := counted(b)
	var out []string
	for _, item := range a {
		if seen[item] > 0 {
			seen[item]--
			continue
		}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func counted(items []string) map[string]int {
	m := map[string]int{}
	for _, item := range items {
		m[item]++
	}
	return m
}

func sameSet(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

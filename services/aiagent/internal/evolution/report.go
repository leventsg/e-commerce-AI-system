package evolution

import (
	"fmt"
	"strings"
)

func noSourceReport(outcome Outcome, analysis AnalysisOutput) string {
	var b strings.Builder
	b.WriteString("# Self-Evolving 运行报告\n\n")
	b.WriteString("## 结论\n\n")
	b.WriteString("未读取到生产轨迹来源，本次运行未产生候选或评测。\n\n")
	b.WriteString("## 原因\n\n")
	b.WriteString(fmt.Sprintf("- 来源错误：%s\n", outcome.Error))
	b.WriteString(fmt.Sprintf("- 分析窗口：%s ~ %s\n", analysis.WindowFrom, analysis.WindowTo))
	b.WriteString(fmt.Sprintf("- 目标 Agent：%s\n", analysis.TargetAgent))
	b.WriteString("\n## 说明\n\n")
	b.WriteString("该结果不表示生产自进化已通过验证；需要配置 Langfuse 来源与凭据后重跑。\n")
	return b.String()
}

func noCandidateReport(analysis AnalysisOutput) string {
	var b strings.Builder
	b.WriteString("# Self-Evolving 运行报告\n\n")
	b.WriteString("## 结论\n\n")
	b.WriteString("分析未发现明确可修复的 Skill 缺口或可复用经验，本轮输出 no_candidate，未进行 A/B 评测。\n\n")
	b.WriteString("## 分析摘要\n\n")
	if analysis.Gap != "" {
		b.WriteString(fmt.Sprintf("- 能力缺口：%s\n", analysis.Gap))
	}
	if analysis.SuccessPattern != "" {
		b.WriteString(fmt.Sprintf("- 可复用经验：%s\n", analysis.SuccessPattern))
	}
	b.WriteString(fmt.Sprintf("- 已分析会话数：%d\n", len(analysis.Sessions)))
	b.WriteString(fmt.Sprintf("- 跳过会话数：%d\n", len(analysis.Skips)))
	return b.String()
}

func abReport(candidate Candidate, analysis AnalysisOutput, ab ABResult, dataset Dataset) string {
	var b strings.Builder
	b.WriteString("# Self-Evolving A/B 评测报告\n\n")
	b.WriteString("## 结论\n\n")
	b.WriteString(fmt.Sprintf("- **判定**：`%s`\n", ab.Conclusion))
	b.WriteString(fmt.Sprintf("- 操作：`%s`（Skill：`%s`，宿主 Agent：`%s`）\n", candidate.Operation, candidate.SkillName, candidate.TargetAgent))
	b.WriteString(fmt.Sprintf("- 覆盖：`%s`\n", ab.Coverage))
	if len(ab.Reasons) > 0 {
		b.WriteString("- 原因：\n")
		for _, r := range ab.Reasons {
			b.WriteString(fmt.Sprintf("  - %s\n", r))
		}
	}
	b.WriteString("\n## 指标\n\n")
	m := ab.Metrics
	b.WriteString("| 指标 | A 组（原版） | B 组（候选） |\n|---|---|---|\n")
	b.WriteString(fmt.Sprintf("| required 加载率 | %.2f | %.2f |\n", m.RequiredLoadRateA, m.RequiredLoadRateB))
	b.WriteString(fmt.Sprintf("| forbidden 误触发率 | %.2f | %.2f |\n", m.ForbiddenTriggerRateA, m.ForbiddenTriggerRateB))
	b.WriteString(fmt.Sprintf("| 核心行为通过率 | %.2f | %.2f |\n", m.BehaviorPassRateA, m.BehaviorPassRateB))
	b.WriteString(fmt.Sprintf("| 安全违规数 | %d | %d |\n", m.SafetyViolationsA, m.SafetyViolationsB))
	b.WriteString(fmt.Sprintf("| 语义裁判 | %s | %s |\n", m.SemanticScoreA, m.SemanticScoreB))
	b.WriteString(fmt.Sprintf("| 平均工具调用 | %.1f | %.1f |\n", m.AvgToolCallsA, m.AvgToolCallsB))
	b.WriteString("\n## 逐例结果\n\n")
	b.WriteString("| 用例 | 类别 | 期望 | A 行为 | B 行为 | 改善 | 退化 |\n|---|---|---|---|---|---|---|\n")
	for _, c := range ab.Cases {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %.2f (%d/%d) | %.2f (%d/%d) | %t | %t |\n",
			c.CaseID, c.Category, c.ExpectedSkillUse,
			behaviorRate(c.GroupA), c.GroupA.BehaviorPass, c.GroupA.BehaviorTotal,
			behaviorRate(c.GroupB), c.GroupB.BehaviorPass, c.GroupB.BehaviorTotal,
			c.Improved, c.Regressed))
	}
	b.WriteString("\n## 数据与限制\n\n")
	b.WriteString(fmt.Sprintf("- 数据集版本：%s（哈希 %s），用例数 %d\n", dataset.Version, ab.DatasetHash, len(dataset.Cases)))
	b.WriteString(fmt.Sprintf("- 每组每用例运行 %d 次，交错执行，状态隔离\n", ab.Trials))
	b.WriteString("- 语义评分为 `unknown` 时表示缺失裁判或证据，未计入通过\n")
	b.WriteString("- 该评测使用模拟业务环境，不访问真实业务 RPC；`improved` 不是正式晋升资格，也不是统计显著结论\n")
	b.WriteString("- 生产轨迹用于学习，测试集用于评测，二者输入隔离\n")
	return b.String()
}

func shadowReport(summary ShadowSummary) string {
	var b strings.Builder
	b.WriteString("# 生产影子执行报告\n\n")
	b.WriteString("## 结论\n\n")
	b.WriteString(fmt.Sprintf("- 会话总数：%d\n", summary.Total))
	b.WriteString(fmt.Sprintf("- 工具一致：%d\n", summary.Consistent))
	b.WriteString(fmt.Sprintf("- 工具不一致：%d\n", summary.ToolMismatch))
	b.WriteString(fmt.Sprintf("- 出现额外写操作（副作用风险）：%d\n", summary.SideEffectRisk))
	b.WriteString(fmt.Sprintf("- 结果改善（原失败/未知 → 影子成功）：%d\n", summary.OutcomeImproved))
	b.WriteString(fmt.Sprintf("- 结果未变差但工具更少：%d\n", summary.ToolReduced))
	b.WriteString("\n## 逐会话结果\n\n")
	b.WriteString("| 会话 | 工具一致 | 结果(原→影子) | 工具次数(原→影子) | 额外写 | 澄清 |\n|---|---|---|---|---|---|\n")
	for _, r := range summary.Results {
		b.WriteString(fmt.Sprintf("| %s | %t | %s→%s | %d→%d | %v | %t |\n",
			r.TraceID, r.ToolsMatch, r.OriginalOutcome, r.ShadowOutcome,
			r.OriginalToolCount, r.ShadowToolCount, r.ExtraShadowWrites, r.AsksClarification))
	}
	b.WriteString("\n## 说明\n\n")
	b.WriteString("- 影子执行从生产轨迹的读操作结果重建数据快照，在模拟环境重放用户输入，写操作不真发业务 RPC。\n")
	b.WriteString("- 下限（安全）：工具一致 + 无额外写操作，保证不退化、不越权、不误触发。\n")
	b.WriteString("- 有效性信号：结果改善（原失败/未知→影子成功）或结果未变差但工具更少，用于判断新 skill 是否真的有效。\n")
	b.WriteString("- 澄清为启发式判断（最终回复含问句与澄清词），不是精确的轮次统计；该对比仍不是真实生产执行的等价证明。\n")
	return b.String()
}

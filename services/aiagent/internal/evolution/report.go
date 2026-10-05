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

func regressionReport(report RegressionReport) string {
	var b strings.Builder
	b.WriteString("## 回归校验\n\n")
	b.WriteString(fmt.Sprintf("- 结论：`%s`\n", report.Conclusion))
	b.WriteString(fmt.Sprintf("- 回归集版本：%s（哈希 %s），匹配用例 %d\n", report.Version, report.Hash, report.MatchedCases))
	b.WriteString(fmt.Sprintf("- 每组每例运行 %d 次（A 原版 / B 候选交错）\n", report.Trials))
	if len(report.RegressedCases) > 0 {
		b.WriteString(fmt.Sprintf("- 退化用例：%v\n", report.RegressedCases))
	}
	b.WriteString("\n| 用例 | 能力 | A 通过 | B 通过 | 判定 |\n|---|---|---|---|---|\n")
	for _, c := range report.Cases {
		verdict := "无退化"
		if c.Skipped {
			verdict = "跳过（原版基准不成立）"
		} else if c.Regressed {
			verdict = "退化"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %d/%d | %d/%d | %s |\n", c.CaseID, c.Capability, c.PassA, c.Runs, c.PassB, c.Runs, verdict))
	}
	b.WriteString("\n- 回归护栏回答「候选有没有把原本正常的能力改坏」，与重放校验回答「失败 case 有没有修好」互补。\n")
	b.WriteString("- 若 B 组在任一典型能力上通过率下降，即使重放显示失败 case 已修复，也应判「不能采纳」。\n")
	return b.String()
}

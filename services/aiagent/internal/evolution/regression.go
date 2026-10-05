package evolution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

// RegressionCase 是回归测试集里的一个典型能力样例，用于验证候选 skill 不会让
// 原本正常的能力退化。它与生产轨迹无关，是固定维护的「已知能力的回归护栏」：
// 只验「已知的已知」，不验「未知的新能力」（那由重放校验负责）。
type RegressionCase struct {
	ID          string          `json:"id"`
	SkillName   string          `json:"skill_name"`
	TargetAgent string          `json:"target_agent"`
	Capability  string          `json:"capability"`
	Input       string          `json:"input"`
	Fixture     json.RawMessage `json:"fixture"`
	// ExpectTool 是该典型能力对应的核心工具，作为「能力正常完成」的判定。
	ExpectTool string `json:"expect_tool"`
}

// RegressionSet 是固定版本、内容哈希的回归测试集。它独立于生产轨迹维护，生成器
// 不能读取它（本包只在本命令的回归阶段加载，不进入分析/生成 prompt）。
type RegressionSet struct {
	Version string          `json:"version"`
	Cases   []RegressionCase `json:"cases"`
}

// RegressionCaseResult 是单个回归用例的 A/B 结果。
type RegressionCaseResult struct {
	CaseID     string `json:"case_id"`
	Capability string `json:"capability"`
	Runs       int    `json:"runs"`
	PassA      int    `json:"pass_a"` // 原 skill 通过次数
	PassB      int    `json:"pass_b"` // 候选 skill 通过次数
	Skipped    bool   `json:"skipped,omitempty"` // A 组基准不成立（原 skill 本就不通过），跳过
	Regressed  bool   `json:"regressed"`         // B 组通过率低于 A 组
}

// RegressionReport 是回归校验的总结论。
type RegressionReport struct {
	Version        string                 `json:"version"`
	Hash           string                 `json:"hash"`
	SkillName      string                 `json:"skill_name"`
	TargetAgent    string                 `json:"target_agent"`
	Trials         int                    `json:"trials"`
	MatchedCases   int                    `json:"matched_cases"`
	Conclusion     string                 `json:"conclusion"` // not_regressed | regressed | insufficient_coverage
	RegressedCases []string               `json:"regressed_cases,omitempty"`
	Cases          []RegressionCaseResult `json:"cases"`
}

// LoadRegressionSet 加载回归测试集并返回内容哈希（原始字节的 SHA-256），用于固定
// 版本、可审计复现。
func LoadRegressionSet(path string) (*RegressionSet, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read regression set: %w", err)
	}
	var set RegressionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, "", fmt.Errorf("parse regression set: %w", err)
	}
	if err := validateRegressionSet(set); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return &set, hex.EncodeToString(sum[:]), nil
}

func validateRegressionSet(set RegressionSet) error {
	if set.Version == "" {
		return fmt.Errorf("regression set version required")
	}
	if len(set.Cases) == 0 {
		return fmt.Errorf("regression set must contain at least one case")
	}
	for _, c := range set.Cases {
		if c.ID == "" || c.SkillName == "" || c.Input == "" || c.ExpectTool == "" {
			return fmt.Errorf("regression case %q missing required fields (id/skill_name/input/expect_tool)", c.ID)
		}
	}
	return nil
}

// RunRegression 对候选 skill 跑回归校验：A 组用内嵌原版（空 SkillSet），B 组用候选
// 版，对比每个典型能力用例的通过率，判断候选是否让原本正常的能力退化。
//
// 判定：B 组通过率严格低于 A 组 → 退化；A 组通过率为 0 → 该用例不能作为基准，跳过；
// 没有匹配候选 skill 的用例 → insufficient_coverage（不假装通过）。
func RunRegression(ctx context.Context, cfg ABConfig, set *RegressionSet, hash string, candidate Candidate, trials int) (RegressionReport, error) {
	if cfg.TestedFactory == nil {
		return RegressionReport{}, fmt.Errorf("tested model factory is required")
	}
	if trials < 1 {
		trials = 1
	}
	skillSetB, err := candidate.SkillSet()
	if err != nil {
		return RegressionReport{}, err
	}
	report := RegressionReport{
		Version:     set.Version,
		Hash:        hash,
		SkillName:   candidate.SkillName,
		TargetAgent: candidate.TargetAgent,
		Trials:      trials,
		Cases:       []RegressionCaseResult{},
	}
	for _, c := range set.Cases {
		if c.SkillName != candidate.SkillName {
			continue // 只回归候选 skill 自己的典型能力（A/B 只替换了这一个 skill）
		}
		passA, passB := 0, 0
		for i := 0; i < trials; i++ {
			// A/B 交错，避免顺序/时间偏差。
			if regressionRun(ctx, cfg, c, eino.SkillSet{}, fmt.Sprintf("rega_%s_%d", c.ID, i)) {
				passA++
			}
			if regressionRun(ctx, cfg, c, skillSetB, fmt.Sprintf("regb_%s_%d", c.ID, i)) {
				passB++
			}
		}
		result := RegressionCaseResult{CaseID: c.ID, Capability: c.Capability, Runs: trials, PassA: passA, PassB: passB}
		if passA == 0 {
			result.Skipped = true
		} else if passB < passA {
			result.Regressed = true
		}
		report.Cases = append(report.Cases, result)
		if result.Regressed {
			report.RegressedCases = append(report.RegressedCases, c.ID)
		}
	}
	report.MatchedCases = len(report.Cases)
	report.Conclusion = concludeRegression(report.MatchedCases, report.RegressedCases)
	return report, nil
}

// concludeRegression 根据匹配用例数与退化用例给出三态结论：没有匹配候选 skill 的
// 用例 → insufficient_coverage（不假装通过）；有退化 → regressed；否则 not_regressed。
func concludeRegression(matched int, regressedCases []string) string {
	switch {
	case matched == 0:
		return "insufficient_coverage"
	case len(regressedCases) > 0:
		return "regressed"
	default:
		return "not_regressed"
	}
}

// regressionRun 跑单个用例的单个 run，返回是否通过：核心工具被调用且执行无错误。
func regressionRun(ctx context.Context, cfg ABConfig, c RegressionCase, skillSet eino.SkillSet, trialID string) bool {
	req := eino.EvaluationRequest{Op: "chat", TrialID: trialID, Content: c.Input, Fixture: c.Fixture}
	trial, err := eino.NewEvaluationTrial(ctx, req, cfg.TestedConfig, cfg.TestedFactory, nil, eino.WithSkillSet(skillSet))
	if err != nil {
		return false
	}
	resp := trial.Execute(ctx, req)
	if resp.Error != "" {
		return false
	}
	for _, tr := range resp.Trajectory {
		if tr.Kind == "tool" && tr.Tool == c.ExpectTool {
			return true
		}
	}
	return false
}

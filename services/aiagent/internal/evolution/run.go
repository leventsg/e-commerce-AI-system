package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/skills"
)

// Options configures a single run. Credentials are injected through environment
// or existing config and are never written to artifacts.
type Options struct {
	From         time.Time
	To           time.Time
	Agent        string
	MaxSessions  int
	DatasetPath  string
	Trials       int
	OutputDir    string
	EvaluateOnly bool
	CandidatePath string

	Langfuse   config.LangfuseConfig
	Analysis   config.EinoConfig
	Generation config.EinoConfig
	Tested     config.EinoConfig
	Judge      config.EinoConfig
	JudgeEnabled bool
}

// Outcome reports what a run produced and where it wrote artifacts.
type Outcome struct {
	RunID     string     `json:"run_id"`
	OutputDir string     `json:"output_dir"`
	Stage     string     `json:"stage"` // no_candidate | evaluated | error | no_source
	Conclusion string    `json:"conclusion,omitempty"`
	Files     []string   `json:"files,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// Run executes the full pipeline. It preserves completed artifacts on failure and
// never claims the whole round succeeded when a stage failed.
func Run(ctx context.Context, opts Options) Outcome {
	runID := time.Now().UTC().Format("20060102T150405.000000000")
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = ".tmp/skill-evolution"
	}
	outDir = filepath.Join(outDir, runID)
	outcome := Outcome{RunID: runID, OutputDir: outDir}

	dataset, datasetHash, err := LoadDataset(opts.DatasetPath)
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = err.Error()
		return outcome
	}

	modelFactory := eino.NewModelFactory()
	var candidate *Candidate

	if opts.EvaluateOnly {
		candidate, err = loadCandidate(opts.CandidatePath, outDir)
		if err != nil {
			outcome.Stage = "error"
			outcome.Error = err.Error()
			return outcome
		}
	} else {
		// 1. Read production evidence.
		sessions, sourceErr := readProductionSessions(ctx, opts)
		if sourceErr != nil {
			// Preserve the error in analysis but continue to a no_source stage.
			outcome.Stage = "no_source"
			outcome.Error = sourceErr.Error()
		}
		// 2. Independent analysis.
		analysis, analyzeErr := AnalyzeProduction(ctx, modelFactory, opts.Analysis, sessions, opts.From.Format(time.RFC3339), opts.To.Format(time.RFC3339), opts.Agent, opts.MaxSessions)
		if analyzeErr != nil {
			outcome.Stage = "error"
			outcome.Error = "analysis failed: " + analyzeErr.Error()
			_ = writeJSON(outDir, "analysis.json", analysis)
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
			return outcome
		}
		_ = writeJSON(outDir, "analysis.json", analysis)
		if outcome.Stage == "no_source" {
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
			_ = writeMarkdown(outDir, "report.md", noSourceReport(outcome, analysis))
			return outcome
		}
		// 3. Candidate generation.
		candidate, err = GenerateCandidate(ctx, modelFactory, opts.Generation, analysis)
		if err != nil {
			outcome.Stage = "error"
			outcome.Error = "candidate generation failed: " + err.Error()
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
			return outcome
		}
		if candidate == nil {
			outcome.Stage = "no_candidate"
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage})
			_ = writeMarkdown(outDir, "report.md", noCandidateReport(analysis))
			return outcome
		}
		_ = writeJSON(outDir, "candidate.json", candidate)
		_ = writeSkillFiles(outDir, *candidate)
	}

	// 4. A/B evaluation.
	ab, err := RunAB(ctx, dataset, datasetHash, *candidate, ABConfig{
		TestedFactory: modelFactory,
		TestedConfig:  opts.Tested,
		JudgeEnabled:  opts.JudgeEnabled,
		JudgeFactory:  modelFactory,
		JudgeConfig:   opts.Judge,
		Trials:        opts.Trials,
	})
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = "A/B evaluation failed: " + err.Error()
		return outcome
	}
	outcome.Stage = "evaluated"
	outcome.Conclusion = ab.Conclusion
	_ = writeJSON(outDir, "ab-results.json", ab)
	_ = writeManifest(outDir, manifest{
		Stage:          outcome.Stage,
		DatasetVersion: dataset.Version,
		DatasetHash:    datasetHash,
		Candidate:      candidate,
		Trials:         opts.Trials,
		ModelConfigs:   modelConfigView(opts),
	})
	_ = writeMarkdown(outDir, "report.md", abReport(*candidate, analysisView(outcome), ab, dataset))
	return outcome
}

func analysisView(outcome Outcome) AnalysisOutput {
	// Best-effort: the analysis file is already on disk; the report reads it again.
	var analysis AnalysisOutput
	raw, err := os.ReadFile(filepath.Join(outcome.OutputDir, "analysis.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &analysis)
	}
	return analysis
}

func readProductionSessions(ctx context.Context, opts Options) ([]SessionEvidence, error) {
	reader := &LangfuseReader{Host: opts.Langfuse.Host, PublicKey: opts.Langfuse.PublicKey, SecretKey: opts.Langfuse.SecretKey}
	return reader.FetchSessions(ctx, opts.From, opts.To, opts.Agent, opts.MaxSessions)
}

func loadCandidate(path, outDir string) (*Candidate, error) {
	candidatePath := path
	if candidatePath == "" {
		candidatePath = filepath.Join(outDir, "candidate.json")
	}
	raw, err := os.ReadFile(candidatePath)
	if err != nil {
		return nil, fmt.Errorf("read candidate: %w", err)
	}
	var candidate Candidate
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return nil, fmt.Errorf("parse candidate: %w", err)
	}
	if err := ValidateCandidate(candidate); err != nil {
		return nil, fmt.Errorf("candidate static check failed: %w", err)
	}
	return &candidate, nil
}

func writeSkillFiles(outDir string, candidate Candidate) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "SKILL.md"), []byte(candidate.SkillMarkdown), 0o644); err != nil {
		return err
	}
	baseline := ""
	if candidate.Operation == "modify" {
		if document, ok := skills.ByName(candidate.SkillName); ok {
			baseline = "---\nname: " + document.Name + "\ndescription: " + document.Description + "\n---\n\n" + document.Content
		}
	}
	return os.WriteFile(filepath.Join(outDir, "skill.diff"), []byte(skillDiff(candidate.Operation, candidate.SkillName, baseline, candidate.SkillMarkdown)), 0o644)
}

func writeJSON(outDir, name string, value any) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name), append(raw, '\n'), 0o644)
}

func writeMarkdown(outDir, name, content string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, name), []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

type manifest struct {
	Stage          string            `json:"stage"`
	Error          string            `json:"error,omitempty"`
	DatasetVersion string            `json:"dataset_version,omitempty"`
	DatasetHash    string            `json:"dataset_hash,omitempty"`
	Candidate      *Candidate        `json:"candidate,omitempty"`
	Trials         int               `json:"trials,omitempty"`
	ModelConfigs   map[string]string `json:"model_configs,omitempty"`
	GeneratedAt    string            `json:"generated_at"`
}

func writeManifest(outDir string, m manifest) error {
	m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	return writeJSON(outDir, "manifest.json", m)
}

// RunShadowOnly 读取生产轨迹并对每个会话做影子执行，输出 shadow-results.json
// 与 shadow-report.md。影子执行不真发业务 RPC，写操作只记录不执行。
func RunShadowOnly(ctx context.Context, opts Options) Outcome {
	runID := time.Now().UTC().Format("20060102T150405.000000000")
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = ".tmp/skill-evolution"
	}
	outDir = filepath.Join(outDir, runID)
	outcome := Outcome{RunID: runID, OutputDir: outDir, Stage: "shadow"}

	sessions, err := readProductionSessions(ctx, opts)
	if err != nil {
		outcome.Stage = "no_source"
		outcome.Error = err.Error()
		_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
		return outcome
	}

	var skillSet eino.SkillSet
	if opts.CandidatePath != "" {
		candidate, loadErr := loadCandidate(opts.CandidatePath, outDir)
		if loadErr != nil {
			outcome.Stage = "error"
			outcome.Error = loadErr.Error()
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
			return outcome
		}
		skillSet, _ = candidate.SkillSet()
	}

	summary, err := RunShadow(ctx, ABConfig{
		TestedFactory: eino.NewModelFactory(),
		TestedConfig:  opts.Tested,
	}, sessions, skillSet)
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = err.Error()
		_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
		return outcome
	}
	outcome.Conclusion = fmt.Sprintf("%d/%d consistent, %d side-effect risk", summary.Consistent, summary.Total, summary.SideEffectRisk)
	_ = writeJSON(outDir, "shadow-results.json", summary)
	_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Trials: opts.Trials, ModelConfigs: modelConfigView(opts)})
	_ = writeMarkdown(outDir, "shadow-report.md", shadowReport(summary))
	return outcome
}

// RunGenericEval 对候选 Skill 运行四层通用评测（触发/安全/有效性/鲁棒性），不依赖
// skill-specific 数据集。它读取候选，从 SKILL.md 提取画像并生成通用用例，输出
// generic-report.json 与 generic-report.md。
func RunGenericEval(ctx context.Context, opts Options) Outcome {
	runID := time.Now().UTC().Format("20060102T150405.000000000")
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = ".tmp/skill-evolution"
	}
	outDir = filepath.Join(outDir, runID)
	outcome := Outcome{RunID: runID, OutputDir: outDir, Stage: "generic"}

	candidate, err := loadCandidate(opts.CandidatePath, outDir)
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = err.Error()
		_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
		return outcome
	}

	factory := eino.NewModelFactory()
	report, err := GenericEval(ctx, *candidate, ABConfig{
		TestedFactory: factory,
		TestedConfig:  opts.Tested,
		JudgeEnabled:  opts.JudgeEnabled,
		JudgeFactory:  factory,
		JudgeConfig:   opts.Judge,
		Trials:        opts.Trials,
	}, opts.Trials)
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = err.Error()
		_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error})
		return outcome
	}
	outcome.Conclusion = report.Conclusion
	_ = writeJSON(outDir, "generic-report.json", report)
	_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Trials: opts.Trials, Candidate: candidate, ModelConfigs: modelConfigView(opts)})
	_ = writeMarkdown(outDir, "generic-report.md", genericReportMarkdown(report))
	return outcome
}

// genericReportMarkdown renders the four-layer generic evaluation as Markdown.
func genericReportMarkdown(r GenericReport) string {
	var b strings.Builder
	b.WriteString("# 通用 Skill 评测报告\n\n")
	b.WriteString(fmt.Sprintf("- Skill：%s（只读声明：%t）\n", r.SkillName, r.ReadOnly))
	b.WriteString(fmt.Sprintf("- 适用样例：%s\n", r.ApplicableSample))
	b.WriteString(fmt.Sprintf("- 每组每例次数：%d\n\n", r.Trials))
	b.WriteString(fmt.Sprintf("## 结论：%s\n\n", r.Conclusion))
	b.WriteString("## 触发边界\n\n| 指标 | A 组 | B 组 |\n|---|---|---|\n")
	b.WriteString(fmt.Sprintf("| 适用场景加载率 | %.0f%% | %.0f%% |\n", r.Trigger.ApplicableLoadRateA*100, r.Trigger.ApplicableLoadRateB*100))
	b.WriteString(fmt.Sprintf("| 不适用误触发率 | %.0f%% | %.0f%% |\n\n", r.Trigger.NotApplicableTriggerRateA*100, r.Trigger.NotApplicableTriggerRateB*100))
	b.WriteString("## 安全\n\n| 指标 | A 组 | B 组 |\n|---|---|---|\n")
	b.WriteString(fmt.Sprintf("| 写违规 | %d | %d |\n", r.Safety.WriteViolationsA, r.Safety.WriteViolationsB))
	b.WriteString(fmt.Sprintf("| 注入抵抗 | %s | %s |\n", r.Safety.InjectionResistA, r.Safety.InjectionResistB))
	b.WriteString(fmt.Sprintf("| 越权抵抗 | %s | %s |\n\n", r.Safety.OverreachResistA, r.Safety.OverreachResistB))
	b.WriteString("## 有效性\n\n| 指标 | A 组 | B 组 |\n|---|---|---|\n")
	b.WriteString(fmt.Sprintf("| 平均工具调用 | %.2f | %.2f |\n", r.Effectiveness.AvgToolCallsA, r.Effectiveness.AvgToolCallsB))
	b.WriteString(fmt.Sprintf("| 澄清比例 | %.0f%% | %.0f%% |\n\n", r.Effectiveness.ClarificationRateA*100, r.Effectiveness.ClarificationRateB*100))
	b.WriteString("## 鲁棒性\n\n| 指标 | A 组 | B 组 |\n|---|---|---|\n")
	b.WriteString(fmt.Sprintf("| 信息不足标 unknown | %s | %s |\n", r.Robustness.InsufficientUnknownA, r.Robustness.InsufficientUnknownB))
	b.WriteString(fmt.Sprintf("| 能力外正确拒绝 | %s | %s |\n", r.Robustness.OutOfScopeRejectA, r.Robustness.OutOfScopeRejectB))
	return b.String()
}

// modelConfigView records model names (never credentials) for reproducibility.
func modelConfigView(opts Options) map[string]string {
	out := map[string]string{}
	if opts.Analysis.Model != "" {
		out["analysis"] = opts.Analysis.Model
	}
	if opts.Generation.Model != "" {
		out["generation"] = opts.Generation.Model
	}
	if opts.Tested.Model != "" {
		out["tested"] = opts.Tested.Model
	}
	if opts.JudgeEnabled && opts.Judge.Model != "" {
		out["judge"] = opts.Judge.Model
	}
	return out
}

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
	From           time.Time
	To             time.Time
	Agent          string
	MaxSessions    int
	TracesFile     string // 本地轨迹文件（SessionEvidence 数组 JSON），无 Langfuse 时用于端到端验证
	RegressionPath string // 回归测试集路径（可选；提供则在重放校验后追加回归护栏）
	Trials         int
	OutputDir      string
	EvaluateOnly   bool
	CandidatePath  string

	Langfuse   config.LangfuseConfig
	Analysis   config.EinoConfig
	Generation config.EinoConfig
	Tested     config.EinoConfig
}

// Outcome reports what a run produced and where it wrote artifacts.
type Outcome struct {
	RunID      string   `json:"run_id"`
	OutputDir  string   `json:"output_dir"`
	Stage      string   `json:"stage"` // no_candidate | evaluated | error | no_source
	Conclusion string   `json:"conclusion,omitempty"`
	Files      []string `json:"files,omitempty"`
	Error      string   `json:"error,omitempty"`
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

	modelFactory := eino.NewModelFactory()
	var candidate *Candidate
	var sessions []SessionEvidence
	var err error

	if opts.EvaluateOnly {
		candidate, err = loadCandidate(opts.CandidatePath, outDir)
		if err != nil {
			outcome.Stage = "error"
			outcome.Error = err.Error()
			return outcome
		}
		// 重放需要原轨迹；EvaluateOnly 模式从 traces-file 读（无 Langfuse 时间窗）。
		if opts.TracesFile != "" {
			sessions, err = readTracesFile(opts.TracesFile)
			if err != nil {
				outcome.Stage = "error"
				outcome.Error = err.Error()
				return outcome
			}
		}
	} else {
		// 1. Read production evidence.
		var sourceErr error
		sessions, sourceErr = readProductionSessions(ctx, opts)
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

	// 4. 重放校验（用候选 skill 重放原轨迹，对比工具调用与结果）
	skillSet, _ := candidate.SkillSet()
	summary, err := RunShadow(ctx, ABConfig{
		TestedFactory: modelFactory,
		TestedConfig:  opts.Tested,
	}, sessions, skillSet)
	if err != nil {
		outcome.Stage = "error"
		outcome.Error = "shadow replay failed: " + err.Error()
		return outcome
	}
	outcome.Stage = "evaluated"
	outcome.Conclusion = fmt.Sprintf("tool_reduced=%d/%d, outcome_improved=%d/%d", summary.ToolReduced, summary.Total, summary.OutcomeImproved, summary.Total)
	_ = writeJSON(outDir, "shadow-results.json", summary)

	// 5. 回归校验（可选）：在固定典型能力测试集上跑 A/B，确认候选没有让原本正常的能力退化。
	reportBody := shadowReport(summary)
	if opts.RegressionPath != "" {
		set, hash, loadErr := LoadRegressionSet(opts.RegressionPath)
		if loadErr != nil {
			outcome.Stage = "error"
			outcome.Error = "regression set load failed: " + loadErr.Error()
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error, Candidate: candidate, Trials: opts.Trials, ModelConfigs: modelConfigView(opts)})
			return outcome
		}
		r, runErr := RunRegression(ctx, ABConfig{TestedFactory: modelFactory, TestedConfig: opts.Tested}, set, hash, *candidate, opts.Trials)
		if runErr != nil {
			outcome.Stage = "error"
			outcome.Error = "regression failed: " + runErr.Error()
			_ = writeManifest(outDir, manifest{Stage: outcome.Stage, Error: outcome.Error, Candidate: candidate, Trials: opts.Trials, ModelConfigs: modelConfigView(opts)})
			return outcome
		}
		_ = writeJSON(outDir, "regression-results.json", r)
		outcome.Conclusion = outcome.Conclusion + fmt.Sprintf(", regression=%s", r.Conclusion)
		reportBody += "\n\n" + regressionReport(r)
	}

	_ = writeManifest(outDir, manifest{
		Stage:        outcome.Stage,
		Candidate:    candidate,
		Trials:       opts.Trials,
		ModelConfigs: modelConfigView(opts),
	})
	_ = writeMarkdown(outDir, "report.md", reportBody)
	return outcome
}

func readProductionSessions(ctx context.Context, opts Options) ([]SessionEvidence, error) {
	if opts.TracesFile != "" {
		return readTracesFile(opts.TracesFile)
	}
	reader := &LangfuseReader{Host: opts.Langfuse.Host, PublicKey: opts.Langfuse.PublicKey, SecretKey: opts.Langfuse.SecretKey}
	return reader.FetchSessions(ctx, opts.From, opts.To, opts.Agent, opts.MaxSessions)
}

// readTracesFile 从本地 JSON 读取 SessionEvidence 数组，用于无真实 Langfuse 时的
// 端到端验证（轨迹学习 → 提炼 skill → 重放校验）。
func readTracesFile(path string) ([]SessionEvidence, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read traces file: %w", err)
	}
	var sessions []SessionEvidence
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, fmt.Errorf("parse traces file: %w", err)
	}
	return sessions, nil
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
	return out
}

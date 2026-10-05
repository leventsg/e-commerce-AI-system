// skillevolve runs a single Self-Evolving pass: read redacted production
// Langfuse traces, derive one Skill candidate, and A/B it against the fixed
// Skill-specific dataset. It writes all artifacts locally and never modifies the
// production Skill set. Credentials come from environment variables and are never
// written into artifacts.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/evolution"
	"github.com/zeromicro/go-zero/core/logx"
)

func main() {
	logx.SetWriter(logx.NewWriter(os.Stderr))
	var (
		from         = flag.String("from", "", "production window start (RFC3339, required)")
		to           = flag.String("to", "", "production window end (RFC3339, required)")
		agent        = flag.String("agent", "", "target agent")
		maxSessions  = flag.Int("max-sessions", 20, "max sessions to analyze")
		dataset      = flag.String("dataset", "tests/skill_eval/dataset.json", "skill-specific dataset path")
		trials       = flag.Int("trials", 3, "trials per case per group")
		output       = flag.String("output", ".tmp/skill-evolution", "output directory")
		evaluateOnly = flag.Bool("evaluate-only", false, "re-run A/B for an existing candidate")
		shadow       = flag.Bool("shadow", false, "shadow-execute production traces (no real business RPC)")
		genericEval  = flag.Bool("generic-eval", false, "run four-layer generic evaluation for any skill")
		candidate    = flag.String("candidate", "", "candidate.json path (evaluate-only / shadow / generic-eval)")
	)
	flag.Parse()

	opts := evolution.Options{
		Agent:        *agent,
		MaxSessions:  *maxSessions,
		DatasetPath:  *dataset,
		Trials:       *trials,
		OutputDir:    *output,
		EvaluateOnly: *evaluateOnly,
		CandidatePath: *candidate,
		Langfuse: config.LangfuseConfig{
			Host:      os.Getenv("AI_EVOLVE_LANGFUSE_HOST"),
			PublicKey: os.Getenv("AI_EVOLVE_LANGFUSE_PUBLIC_KEY"),
			SecretKey: os.Getenv("AI_EVOLVE_LANGFUSE_SECRET_KEY"),
		},
		Analysis: config.EinoConfig{
			Provider: "openai", APIKey: os.Getenv("AI_EVOLVE_ANALYSIS_API_KEY"),
			Model: os.Getenv("AI_EVOLVE_ANALYSIS_MODEL"), BaseURL: os.Getenv("AI_EVOLVE_ANALYSIS_BASE_URL"),
			Timeout: 60, MaxTokens: 4096,
		},
		Generation: config.EinoConfig{
			Provider: "openai", APIKey: os.Getenv("AI_EVOLVE_GEN_API_KEY"),
			Model: os.Getenv("AI_EVOLVE_GEN_MODEL"), BaseURL: os.Getenv("AI_EVOLVE_GEN_BASE_URL"),
			Timeout: 60, MaxTokens: 4096,
		},
		Tested: config.EinoConfig{
			Provider: "openai", APIKey: os.Getenv("AI_EVAL_API_KEY"),
			Model: os.Getenv("AI_EVAL_MODEL"), BaseURL: os.Getenv("AI_EVAL_BASE_URL"),
			Timeout: 60, MaxTokens: 4096,
		},
		Judge: config.EinoConfig{
			Provider: "openai", APIKey: os.Getenv("AI_EVAL_JUDGE_API_KEY"),
			Model: os.Getenv("AI_EVAL_JUDGE_MODEL"), BaseURL: os.Getenv("AI_EVAL_JUDGE_BASE_URL"),
			Timeout: 60, MaxTokens: 2048,
		},
		JudgeEnabled: os.Getenv("AI_EVAL_JUDGE_API_KEY") != "" && os.Getenv("AI_EVAL_JUDGE_MODEL") != "",
	}

	if !*evaluateOnly && !*genericEval {
		if *from == "" || *to == "" {
			fmt.Fprintln(os.Stderr, "--from and --to are required unless --evaluate-only or --generic-eval is set")
			os.Exit(2)
		}
		opts.From, _ = time.Parse(time.RFC3339, *from)
		opts.To, _ = time.Parse(time.RFC3339, *to)
		if opts.From.IsZero() || opts.To.IsZero() || !opts.To.After(opts.From) {
			fmt.Fprintln(os.Stderr, "--from/--to must be valid RFC3339 timestamps with --to after --from")
			os.Exit(2)
		}
	}

	var outcome evolution.Outcome
	if *genericEval {
		outcome = evolution.RunGenericEval(context.Background(), opts)
	} else if *shadow {
		outcome = evolution.RunShadowOnly(context.Background(), opts)
	} else {
		outcome = evolution.Run(context.Background(), opts)
	}
	fmt.Printf("run_id=%s\nstage=%s\nconclusion=%s\noutput_dir=%s\n", outcome.RunID, outcome.Stage, outcome.Conclusion, outcome.OutputDir)
	if outcome.Error != "" {
		fmt.Printf("error=%s\n", outcome.Error)
	}
	if outcome.Stage == "error" {
		os.Exit(1)
	}
}

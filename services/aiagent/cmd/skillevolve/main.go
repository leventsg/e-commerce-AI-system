// skillevolve runs a single Self-Evolving pass: read redacted production traces,
// derive one Skill candidate from failure/detour experience, and replay the
// original traces to verify the candidate's effectiveness. It writes all artifacts
// locally and never modifies the production Skill set. Credentials come from
// environment variables and are never written into artifacts.
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
		from         = flag.String("from", "", "production window start (RFC3339, required unless --traces-file)")
		to           = flag.String("to", "", "production window end (RFC3339, required unless --traces-file)")
		agent        = flag.String("agent", "", "target agent")
		maxSessions  = flag.Int("max-sessions", 20, "max sessions to analyze")
		trials       = flag.Int("trials", 3, "reserved")
		output       = flag.String("output", ".tmp/skill-evolution", "output directory")
		evaluateOnly = flag.Bool("evaluate-only", false, "replay verification for an existing candidate")
		shadow       = flag.Bool("shadow", false, "shadow-replay production traces only (no analysis/generation)")
		candidate    = flag.String("candidate", "", "candidate.json path (evaluate-only / shadow)")
		tracesFile   = flag.String("traces-file", "", "local SessionEvidence JSON file (closed-loop verify without Langfuse)")
		regression   = flag.String("regression", "", "regression test set path (optional; add regression guard after replay)")
	)
	flag.Parse()

	opts := evolution.Options{
		Agent:         *agent,
		MaxSessions:   *maxSessions,
		TracesFile:    *tracesFile,
		RegressionPath: *regression,
		Trials:        *trials,
		OutputDir:     *output,
		EvaluateOnly:  *evaluateOnly,
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
	}

	if !*evaluateOnly && *tracesFile == "" {
		if *from == "" || *to == "" {
			fmt.Fprintln(os.Stderr, "--from and --to are required unless --evaluate-only or --traces-file is set")
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
	if *shadow {
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

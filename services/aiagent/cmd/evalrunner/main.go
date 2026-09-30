// evalrunner is a JSONL-only, local simulation process. It never constructs the
// production service context and cannot dispatch a business RPC.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
	"github.com/zeromicro/go-zero/core/logx"
)

func main() {
	// Library diagnostics must never corrupt the stdout protocol.
	logx.SetWriter(logx.NewWriter(os.Stderr))
	ctx := context.Background()
	observation, err := eino.NewSyntheticObservation(ctx, config.LangfuseConfig{Enabled: os.Getenv("AI_EVAL_LANGFUSE_ENABLED") == "true", Host: os.Getenv("AI_EVAL_LANGFUSE_HOST"), PublicKey: os.Getenv("AI_EVAL_LANGFUSE_PUBLIC_KEY"), SecretKey: os.Getenv("AI_EVAL_LANGFUSE_SECRET_KEY"), UserHashSalt: os.Getenv("AI_EVAL_LANGFUSE_USER_HASH_SALT"), SampleRate: 1, ContentMode: "synthetic", Environment: "evaluation", Release: os.Getenv("AI_EVAL_RELEASE")})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = observation.Shutdown(flush)
	}()
	cfg := config.EinoConfig{Provider: "openai", APIKey: os.Getenv("AI_EVAL_API_KEY"), Model: os.Getenv("AI_EVAL_MODEL"), BaseURL: os.Getenv("AI_EVAL_BASE_URL"), Timeout: 60, MaxTokens: 4096}
	trials := map[string]*eino.EvaluationTrial{}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	writer := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req eino.EvaluationRequest
		if err = json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = writer.Encode(map[string]any{"error": "invalid JSON: " + err.Error()})
			continue
		}
		if req.Op == "schema" {
			_ = writer.Encode(eino.EvaluationSchema())
			continue
		}
		trial := trials[req.TrialID]
		if req.Op == "init" {
			if trial != nil {
				_ = writer.Encode(map[string]any{"trial_id": req.TrialID, "error": "trial already exists"})
				continue
			}
			if cfg.APIKey == "" || cfg.Model == "" {
				_ = writer.Encode(map[string]any{"trial_id": req.TrialID, "error": "AI_EVAL_API_KEY and AI_EVAL_MODEL are required"})
				continue
			}
			trial, err = eino.NewEvaluationTrial(ctx, req, cfg, nil, observation)
			if err != nil {
				_ = writer.Encode(map[string]any{"trial_id": req.TrialID, "error": err.Error()})
				continue
			}
			trials[req.TrialID] = trial
		}
		if trial == nil {
			_ = writer.Encode(map[string]any{"trial_id": req.TrialID, "error": "unknown trial; initialize first"})
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		result := trial.Execute(requestCtx, req)
		cancel()
		_ = writer.Encode(result)
		if req.Op == "end" {
			delete(trials, req.TrialID)
		}
	}
	if err = scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

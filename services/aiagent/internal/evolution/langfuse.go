package evolution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LangfuseReader pages redacted production traces and their observations from a
// Langfuse project using the public REST API. It is a learning source reader and
// never writes. It has not been validated against a live instance in this repo.
type LangfuseReader struct {
	Host       string
	PublicKey  string
	SecretKey  string
	HTTPClient *http.Client
}

type langfuseTrace struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Name      string         `json:"name"`
	UserID    string         `json:"userId"`
	SessionID string         `json:"sessionId"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Metadata  map[string]any `json:"metadata"`
}

type langfuseObservation struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output"`
	Metadata map[string]any `json:"metadata"`
}

// FetchSessions reads at most max sessions in [from, to] for the target agent,
// distinguishing evolution/evaluation traces from production learning sources.
func (r *LangfuseReader) FetchSessions(ctx context.Context, from, to time.Time, agent string, max int) ([]SessionEvidence, error) {
	if r == nil || strings.TrimSpace(r.Host) == "" || r.PublicKey == "" || r.SecretKey == "" {
		return nil, fmt.Errorf("Langfuse host and credentials are required")
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	sessions := []SessionEvidence{}
	page := 1
	for {
		traces, totalPages, err := r.fetchTracesPage(ctx, client, from, to, page)
		if err != nil {
			return nil, err
		}
		for _, trace := range traces {
			if agent != "" && traceAgent(trace) != agent {
				continue
			}
			if isSelfTrace(trace) {
				continue
			}
			evidence, err := r.normalize(ctx, client, trace)
			if err != nil {
				// A single unreadable trace must not abort the whole window.
				evidence = SessionEvidence{TraceID: trace.ID, SessionID: trace.SessionID, Agent: traceAgent(trace), Missing: true}
			}
			sessions = append(sessions, evidence)
			if max > 0 && len(sessions) >= max {
				return sessions, nil
			}
		}
		if page >= totalPages {
			break
		}
		page++
	}
	return sessions, nil
}

func (r *LangfuseReader) fetchTracesPage(ctx context.Context, client *http.Client, from, to time.Time, page int) ([]langfuseTrace, int, error) {
	u, err := url.Parse(strings.TrimRight(r.Host, "/") + "/api/public/traces")
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("fromTimestamp", from.UTC().Format(time.RFC3339))
	q.Set("toTimestamp", to.UTC().Format(time.RFC3339))
	q.Set("limit", "50")
	q.Set("page", fmt.Sprint(page))
	q.Set("orderBy", "timestamp.desc")
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(r.PublicKey+":"+r.SecretKey)))
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Langfuse traces request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("Langfuse traces returned %d", resp.StatusCode)
	}
	var pageResp struct {
		Data []langfuseTrace `json:"data"`
		Meta struct {
			TotalPages int `json:"totalPages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &pageResp); err != nil {
		return nil, 0, fmt.Errorf("Langfuse traces response parse: %w", err)
	}
	return pageResp.Data, pageResp.Meta.TotalPages, nil
}

func (r *LangfuseReader) normalize(ctx context.Context, client *http.Client, trace langfuseTrace) (SessionEvidence, error) {
	evidence := SessionEvidence{
		TraceID:   trace.ID,
		SessionID: trace.SessionID,
		Agent:     traceAgent(trace),
		Input:     string(trace.Input),
		Output:    string(trace.Output),
	}
	if strings.Contains(evidence.Input, `"content_omitted":true`) {
		evidence.Truncated = true
	}
	observations, err := r.fetchObservations(ctx, client, trace.ID)
	if err != nil {
		return evidence, err
	}
	for _, obs := range observations {
		kind, _ := obs.Metadata["kind"].(string)
		switch kind {
		case "tool", "tool_call":
			evidence.ToolCalls = append(evidence.ToolCalls, ToolEvidence{
				Tool:       obs.Name,
				Status:     stringValue(obs.Metadata["status"]),
				Outcome:    stringValue(obs.Metadata["business_outcome"]),
				Arguments:  string(obs.Input),
				Result:     string(obs.Output),
				ToolCallID: stringValue(obs.Metadata["tool_call_id"]),
			})
		case "confirmation":
			evidence.Confirmations = append(evidence.Confirmations, ConfirmationEvidence{
				Tool:   obs.Name,
				Status: stringValue(obs.Metadata["status"]),
				ID:     stringValue(obs.Metadata["confirmation_id"]),
			})
		}
	}
	if len(observations) == 0 && strings.TrimSpace(evidence.Input) == "" {
		evidence.Missing = true
	}
	return evidence, nil
}

func (r *LangfuseReader) fetchObservations(ctx context.Context, client *http.Client, traceID string) ([]langfuseObservation, error) {
	u, _ := url.Parse(strings.TrimRight(r.Host, "/") + "/api/public/observations")
	q := u.Query()
	q.Set("traceId", traceID)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(r.PublicKey+":"+r.SecretKey)))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Langfuse observations request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Langfuse observations returned %d", resp.StatusCode)
	}
	var pageResp struct {
		Data []langfuseObservation `json:"data"`
	}
	if err := json.Unmarshal(body, &pageResp); err != nil {
		return nil, fmt.Errorf("Langfuse observations response parse: %w", err)
	}
	return pageResp.Data, nil
}

func traceAgent(trace langfuseTrace) string {
	if agent, ok := trace.Metadata["agent"].(string); ok && agent != "" {
		return agent
	}
	if agent, ok := trace.Metadata["agent_name"].(string); ok && agent != "" {
		return agent
	}
	return trace.Name
}

func isSelfTrace(trace langfuseTrace) bool {
	// Evolution and evaluation traces are not production learning sources.
	if strings.Contains(trace.Name, "evaluation.") || strings.Contains(trace.Name, "skillevolve") {
		return true
	}
	if env, _ := trace.Metadata["environment"].(string); env == "evaluation" {
		return true
	}
	return false
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

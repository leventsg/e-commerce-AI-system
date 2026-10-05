package eino

import (
	"context"
	"errors"
	"time"

	langfuse "github.com/cloudwego/eino-ext/callbacks/langfuse/v2"
	"github.com/cloudwego/eino/callbacks"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Observation owns the official callback's private provider, queue and exporter.
// It never installs global OTel providers or Eino handlers.
type Observation struct {
	handler *langfuse.CallbackHandler
	policy  obs.Policy
}

func NewObservation(ctx context.Context, c config.LangfuseConfig) (*Observation, error) {
	return newObservation(ctx, c, false)
}

// NewSyntheticObservation is only used by the independent evaluation executable.
func NewSyntheticObservation(ctx context.Context, c config.LangfuseConfig) (*Observation, error) {
	return newObservation(ctx, c, true)
}
func newObservation(ctx context.Context, c config.LangfuseConfig, synthetic bool) (*Observation, error) {
	if !c.Enabled || c.SampleRate == 0 {
		return nil, nil
	}
	if c.UserHashSalt == "" {
		return nil, errors.New("Langfuse UserHashSalt is required")
	}
	mode := obs.ModeSummary
	switch c.ContentMode {
	case "", "summary":
	case "redacted":
		// Explicit production opt-in; summary remains the default. Never enabled
		// implicitly, and never borrows the synthetic path to collect production.
		mode = obs.ModeRedacted
	case "synthetic":
		if !synthetic {
			return nil, errors.New("Langfuse content mode synthetic is evaluation only")
		}
		mode = obs.ModeSynthetic
	default:
		return nil, errors.New("Langfuse content mode must be summary, redacted or synthetic")
	}
	p := obs.Policy{Salt: c.UserHashSalt, Mode: mode}
	exporter, err := obs.NewExporter(ctx, c.Host, c.PublicKey, c.SecretKey, p)
	if err != nil {
		return nil, err
	}
	h, err := langfuse.NewHandler(ctx, &langfuse.Config{Host: c.Host, PublicKey: c.PublicKey, SecretKey: c.SecretKey,
		SpanExporter: exporter,
		ServiceName:  "aiagent", Environment: c.Environment, Release: c.Release, SampleRate: c.SampleRate,
		MaxQueueSize: 1024, MaxExportBatchSize: 128, BatchTimeout: time.Second, Timeout: 3 * time.Second,
		RetryConfig:             &langfuse.RetryConfig{Enabled: true, InitialInterval: 200 * time.Millisecond, MaxInterval: time.Second, MaxElapsedTime: 3 * time.Second},
		MaxAttributeValueLength: 32768, MaxSpanAttributeBytes: 131072, MaskFunc: p.Content, CollapseAgentInternalSpans: true})
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, err
	}
	return &Observation{handler: h, policy: p}, nil
}
func (o *Observation) Start(ctx context.Context, name string, userID uint64, conversationID string, input string) (context.Context, func(string)) {
	if o == nil || o.handler == nil || obs.Active(ctx) {
		return ctx, func(string) {}
	}
	// A request is a new trace even when go-zero has attached a Jaeger parent.
	source := trace.SpanContextFromContext(ctx)
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	ctx = o.handler.StartTrace(ctx, langfuse.WithName(name), langfuse.WithUserID(o.policy.UserID(userID)), langfuse.WithSessionID(conversationID), langfuse.WithInput(input))
	ctx = obs.WithTrace(ctx, o.policy)
	if source.IsValid() {
		ctx = obs.Correlate(ctx, map[string]string{"source_trace_id": source.TraceID().String()})
	}
	ctx = callbacks.InitCallbacks(ctx, nil, o.handler)
	return ctx, func(output string) { o.handler.EndTrace(ctx, output) }
}
func (o *Observation) Shutdown(ctx context.Context) error {
	if o == nil {
		return nil
	}
	return o.handler.Shutdown(ctx)
}
func (o *Observation) Flush(ctx context.Context) error {
	if o == nil {
		return nil
	}
	return o.handler.Flush(ctx)
}
func (o *Observation) SetSession(ctx context.Context, id string) {
	if o == nil || !obs.Active(ctx) {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("langfuse.session.id", id))
}

package observability

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewExporter protects status descriptions and exception events as well as the
// callback's masked inputs/outputs. Callback MaskFunc alone does not mask OTEL
// RecordError payloads. The official callback still owns batching/loss reporting.
func NewExporter(ctx context.Context, host, publicKey, secretKey string, p Policy) (sdktrace.SpanExporter, error) {
	u, err := url.Parse(strings.TrimRight(host, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || publicKey == "" || secretKey == "" {
		return nil, errors.New("invalid Langfuse endpoint or missing credentials")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/v1/traces") {
		u.Path += "/api/public/otel/v1/traces"
	}
	e, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(u.String()), otlptracehttp.WithHeaders(map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(publicKey+":"+secretKey)), "x-langfuse-ingestion-version": "4",
	}), otlptracehttp.WithCompression(otlptracehttp.GzipCompression), otlptracehttp.WithTimeout(3*time.Second),
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: true, InitialInterval: 200 * time.Millisecond, MaxInterval: time.Second, MaxElapsedTime: 3 * time.Second}))
	if err != nil {
		return nil, errors.New("cannot initialize Langfuse exporter")
	}
	return &privacyExporter{SpanExporter: e, policy: p}, nil
}

type privacyExporter struct {
	sdktrace.SpanExporter
	policy Policy
}

func (e *privacyExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	safe := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		safe[i] = privateSpan{ReadOnlySpan: s, policy: e.policy}
	}
	if err := e.SpanExporter.ExportSpans(ctx, safe); err != nil {
		return errors.New("Langfuse export failed")
	}
	return nil
}

type privateSpan struct {
	sdktrace.ReadOnlySpan
	policy Policy
}

func (s privateSpan) Status() sdktrace.Status {
	status := s.ReadOnlySpan.Status()
	if status.Description != "" {
		status.Description = s.policy.Content(status.Description)
	}
	return status
}
func (s privateSpan) Events() []sdktrace.Event {
	events := append([]sdktrace.Event(nil), s.ReadOnlySpan.Events()...)
	for i := range events {
		attrs := append([]attribute.KeyValue(nil), events[i].Attributes...)
		for j, a := range attrs {
			if a.Value.Type() == attribute.STRING {
				attrs[j] = attribute.String(string(a.Key), s.policy.Content(a.Value.AsString()))
			}
		}
		events[i].Attributes = attrs
	}
	return events
}

// Package observability contains transport-independent, privacy-safe trace helpers.
package observability

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Policy struct {
	Salt      string
	Synthetic bool
}
type contextKey struct{}
type state struct {
	Policy Policy
	Root   trace.Span
	Fields map[string]string
}

// WithTrace enables helpers only for this isolated Langfuse provider.
func WithTrace(ctx context.Context, policy Policy) context.Context {
	return context.WithValue(ctx, contextKey{}, state{Policy: policy, Root: trace.SpanFromContext(ctx), Fields: map[string]string{}})
}
func Active(ctx context.Context) bool { _, ok := ctx.Value(contextKey{}).(state); return ok }
func Detached(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, false)
}
func TraceID(ctx context.Context) string {
	if !Active(ctx) {
		return ""
	}
	return trace.SpanContextFromContext(ctx).TraceID().String()
}
func (p Policy) UserID(id uint64) string {
	h := hmac.New(sha256.New, []byte(p.Salt))
	_, _ = h.Write([]byte(strconv.FormatUint(id, 10)))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

var credential = regexp.MustCompile(`(?i)(bearer\s+[^\s"\\]+|\bsk-[a-z0-9_-]+|(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|authorization|secret)\s*[:=]\s*[^\s,;]+)`)

func secretKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	for _, part := range []string{"token", "apikey", "password", "secret", "authorization", "credential", "cookie"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}
func scrub(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if secretKey(k) {
				v[k] = "[REDACTED]"
			} else {
				v[k] = scrub(x)
			}
		}
		return v
	case []any:
		for i, x := range v {
			v[i] = scrub(x)
		}
		return v
	case string:
		var nested any
		if json.Unmarshal([]byte(v), &nested) == nil {
			switch nested.(type) {
			case map[string]any, []any:
				b, _ := json.Marshal(scrub(nested))
				return string(b)
			}
		}
		return credential.ReplaceAllString(v, "[REDACTED]")
	default:
		return v
	}
}

// Production collection omits arbitrary text entirely: regex redaction cannot
// reliably identify all addresses/names in natural language. Full payloads are
// explicitly reserved for the standalone synthetic evaluation entry point.
func (p Policy) Content(raw string) string {
	if raw == "" {
		return ""
	}
	if !p.Synthetic {
		return fmt.Sprintf(`{"content_omitted":true,"bytes":%d}`, len(raw))
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		b, _ := json.Marshal(scrub(value))
		return string(b)
	}
	return credential.ReplaceAllString(raw, "[REDACTED]")
}
func Content(ctx context.Context, value any) string {
	s, ok := ctx.Value(contextKey{}).(state)
	if !ok {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return s.Policy.Content(string(raw))
}

// Correlate accepts only internally-generated identifiers and fixed labels.
func Correlate(ctx context.Context, fields map[string]string) context.Context {
	s, ok := ctx.Value(contextKey{}).(state)
	if !ok {
		return ctx
	}
	next := make(map[string]string, len(s.Fields)+len(fields))
	for k, v := range s.Fields {
		next[k] = v
	}
	for k, v := range fields {
		next[k] = v
	}
	s.Fields = next
	for k, v := range fields {
		s.Root.SetAttributes(attribute.String("langfuse.trace.metadata."+k, v))
	}
	return context.WithValue(ctx, contextKey{}, s)
}
func Start(ctx context.Context, name string, fields map[string]string) (context.Context, func(string)) {
	s, ok := ctx.Value(contextKey{}).(state)
	if !ok {
		return ctx, func(string) {}
	}
	attrs := []attribute.KeyValue{attribute.String("langfuse.observation.type", "span")}
	for k, v := range s.Fields {
		attrs = append(attrs, attribute.String("langfuse.observation.metadata."+k, v))
	}
	for k, v := range fields {
		attrs = append(attrs, attribute.String("langfuse.observation.metadata."+k, v))
	}
	child, span := trace.SpanFromContext(ctx).TracerProvider().Tracer("aiagent.business").Start(ctx, name, trace.WithAttributes(attrs...))
	return child, func(status string) {
		span.SetAttributes(attribute.String("langfuse.observation.metadata.status", status))
		if status == "failed" || status == "unknown" || status == "blocked" {
			span.SetStatus(codes.Error, status)
			span.SetAttributes(attribute.String("langfuse.observation.level", "ERROR"))
		}
		span.End()
	}
}
func Event(ctx context.Context, name, status string, fields map[string]string) {
	_, end := Start(ctx, name, fields)
	end(status)
}
func Result(ctx context.Context, status, output string) {
	if !Active(ctx) {
		return
	}
	span := trace.SpanFromContext(ctx)
	var envelope struct {
		BusinessOutcome string `json:"business_outcome"`
		AttemptCount    int    `json:"attempt_count"`
		RetryCount      int    `json:"retry_count"`
	}
	if json.Unmarshal([]byte(output), &envelope) == nil && envelope.BusinessOutcome != "" {
		span.SetAttributes(attribute.String("langfuse.observation.metadata.business_outcome", envelope.BusinessOutcome), attribute.Int("langfuse.observation.metadata.attempt_count", envelope.AttemptCount), attribute.Int("langfuse.observation.metadata.retry_count", envelope.RetryCount))
	}
	span.SetAttributes(attribute.String("langfuse.observation.output", Content(ctx, output)), attribute.String("langfuse.observation.metadata.business_status", status))
	if status == "failed" || status == "unknown" {
		span.SetStatus(codes.Error, status)
		span.SetAttributes(attribute.String("langfuse.observation.level", "ERROR"))
	}
}

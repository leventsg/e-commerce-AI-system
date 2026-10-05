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

// ContentMode selects how free-text and structured payloads are handled before
// export. It is fixed at provider construction and never taken from user input.
type ContentMode string

const (
	// ModeSummary is the production default: arbitrary text is omitted and only
	// byte counts and structured run metadata are retained.
	ModeSummary ContentMode = "summary"
	// ModeRedacted is an explicit production opt-in that keeps redacted dialogue,
	// tool arguments and results while stripping credentials and best-effort
	// personal information. It is never enabled implicitly.
	ModeRedacted ContentMode = "redacted"
	// ModeSynthetic is reserved for the standalone evaluation executable; it keeps
	// content but still filters credentials.
	ModeSynthetic ContentMode = "synthetic"
)

type Policy struct {
	Salt string
	Mode ContentMode
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

// piiPatterns is a best-effort detector for common structured personal
// information. It cannot reliably identify all names or addresses in free text;
// callers must not claim otherwise. Missing/un-redacted PII is handled by the
// analysis layer marking samples as unknown rather than fabricating content.
var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`1[3-9]\d{9}`),                                  // mainland mobile
	regexp.MustCompile(`(?i)[\w.+-]+@[\w-]+\.[\w.-]+`),                 // email
	regexp.MustCompile(`\b\d{17}[\dXx]\b`),                             // resident ID card
	regexp.MustCompile(`\b0\d{2,3}-\d{7,8}\b`),                         // landline with area code
}

func secretKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	for _, part := range []string{"token", "apikey", "password", "secret", "authorization", "credential", "cookie"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// redactValue recursively redacts credential-named keys and, when pii is set,
// personal information inside string values. Nested JSON strings are decoded and
// redacted in place so that no credential or PII escapes inside a nested payload.
func redactValue(value any, pii bool) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if secretKey(k) {
				v[k] = "[REDACTED]"
			} else {
				v[k] = redactValue(x, pii)
			}
		}
		return v
	case []any:
		for i, x := range v {
			v[i] = redactValue(x, pii)
		}
		return v
	case string:
		var nested any
		if json.Unmarshal([]byte(v), &nested) == nil {
			switch nested.(type) {
			case map[string]any, []any:
				b, _ := json.Marshal(redactValue(nested, pii))
				return string(b)
			}
		}
		return redactText(v, pii)
	default:
		return v
	}
}

func redactText(s string, pii bool) string {
	s = credential.ReplaceAllString(s, "[REDACTED]")
	if pii {
		for _, re := range piiPatterns {
			s = re.ReplaceAllString(s, "[REDACTED]")
		}
	}
	return s
}

// redactContent applies the redacted/synthetic content policy. When raw is a JSON
// object or array it is redacted field by field; otherwise it is treated as text.
func redactContent(raw string, pii bool) string {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		switch value.(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(redactValue(value, pii))
			return string(b)
		}
	}
	return redactText(raw, pii)
}

// Content applies the configured content policy.
//   - summary (default) omits arbitrary text entirely: regex redaction cannot
//     reliably identify all addresses/names in natural language.
//   - redacted keeps structured and free-text content but strips credentials and
//     best-effort personal information; it is production opt-in only.
//   - synthetic keeps content and filters credentials (evaluation only).
func (p Policy) Content(raw string) string {
	if raw == "" {
		return ""
	}
	switch p.Mode {
	case ModeSynthetic:
		return redactContent(raw, false)
	case ModeRedacted:
		return redactContent(raw, true)
	default:
		return fmt.Sprintf(`{"content_omitted":true,"bytes":%d}`, len(raw))
	}
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

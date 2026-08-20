package logfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// TerminalWriter 只把带 component 字段的业务日志输出到终端，
// 全量日志仍由 logx 已有的文件 writer 写入文件。
type TerminalWriter struct {
	stdout io.Writer
}

func NewTerminalWriter() logx.Writer {
	return &TerminalWriter{stdout: os.Stdout}
}

func (w *TerminalWriter) Alert(v any) {
	w.write("alert", v)
}

func (w *TerminalWriter) Close() error {
	return nil
}

func (w *TerminalWriter) Debug(v any, fields ...logx.LogField) {
	w.write("debug", v, fields...)
}

func (w *TerminalWriter) Error(v any, fields ...logx.LogField) {
	w.write("error", v, fields...)
}

func (w *TerminalWriter) Info(v any, fields ...logx.LogField) {
	w.write("info", v, fields...)
}

func (w *TerminalWriter) Severe(v any) {
	w.write("severe", v)
}

func (w *TerminalWriter) Slow(v any, fields ...logx.LogField) {
	w.write("slow", v, fields...)
}

func (w *TerminalWriter) Stack(v any) {
	w.write("stack", v)
}

func (w *TerminalWriter) Stat(v any, fields ...logx.LogField) {
	w.write("stat", v, fields...)
}

func (w *TerminalWriter) write(level string, v any, fields ...logx.LogField) {
	if !hasComponent(fields) {
		return
	}
	entry := map[string]any{
		"@timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		"level":      level,
		"content":    fmt.Sprint(v),
	}
	for _, field := range fields {
		if field.Key != "" {
			entry[field.Key] = field.Value
		}
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w.stdout, string(raw))
}

func hasComponent(fields []logx.LogField) bool {
	for _, field := range fields {
		if strings.EqualFold(field.Key, "component") {
			value, ok := field.Value.(string)
			return ok && strings.TrimSpace(value) != ""
		}
	}
	return false
}

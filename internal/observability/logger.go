// Package observability 提供结构化日志、trace、脱敏与指标（H1–H4）。
package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

var (
	defaultOnce   sync.Once
	defaultLogger *slog.Logger
)

// NewLogger 构造 JSON 行日志；所有字符串属性与消息都会脱敏。
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if isSensitiveKey(attr.Key) {
				attr.Value = slog.StringValue("***")
				return attr
			}
			if attr.Value.Kind() == slog.KindString {
				attr.Value = slog.StringValue(protocol.Redact(attr.Value.String()))
			}
			return attr
		},
	})
	return slog.New(handler)
}

// Log 返回进程级默认 JSON logger（级别来自 ALADDIN_LOG_LEVEL）。
func Log() *slog.Logger {
	defaultOnce.Do(func() {
		level := slog.LevelInfo
		switch strings.ToLower(os.Getenv("ALADDIN_LOG_LEVEL")) {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
		defaultLogger = NewLogger(os.Stderr, level)
	})
	return defaultLogger
}

// Fields 是日志关联字段（H1）。
type Fields struct {
	TaskID    string
	StepID    string
	SessionID string
	Attempt   uint32
}

// Logger 在 base 上附加 task_id / step_id / session_id / attempt。
func (f Fields) Logger(base *slog.Logger) *slog.Logger {
	if base == nil {
		base = Log()
	}
	attrs := make([]any, 0, 8)
	if f.TaskID != "" {
		attrs = append(attrs, "task_id", f.TaskID)
	}
	if f.StepID != "" {
		attrs = append(attrs, "step_id", f.StepID)
	}
	if f.SessionID != "" {
		attrs = append(attrs, "session_id", f.SessionID)
	}
	if f.Attempt > 0 {
		attrs = append(attrs, "attempt", f.Attempt)
	}
	return base.With(attrs...)
}

func isSensitiveKey(key string) bool {
	switch strings.ToLower(key) {
	case "token", "secret", "password", "passwd", "authorization",
		"api_key", "apikey", "access_token", "refresh_token", "machine_token", "cookie":
		return true
	default:
		return false
	}
}

package observability

import (
	"context"
	"sync"
	"time"

	"github.com/roarzx/my-aladin/internal/idgen"
)

type spanKey struct{}

// Span 是一个轻量 trace 节点（H2），不依赖外部 tracing SDK。
type Span struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Start    time.Time
	EndedAt  time.Time
	Err      error
	attrs    map[string]any
	mu       sync.Mutex
}

// StartSpan 创建 span；同一 ctx 链共享 TraceID。
func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	span := &Span{
		TraceID: idgen.New(), SpanID: idgen.New(), Name: name,
		Start: time.Now().UTC(), attrs: make(map[string]any),
	}
	if parent, ok := SpanFrom(ctx); ok {
		span.TraceID = parent.TraceID
		span.ParentID = parent.SpanID
	}
	return context.WithValue(ctx, spanKey{}, span), span
}

// SpanFrom 取出当前 span。
func SpanFrom(ctx context.Context) (*Span, bool) {
	span, ok := ctx.Value(spanKey{}).(*Span)
	return span, ok
}

// TraceID 返回当前 trace ID；没有 span 时为空。
func TraceID(ctx context.Context) string {
	if span, ok := SpanFrom(ctx); ok {
		return span.TraceID
	}
	return ""
}

// SetAttr 记录 span 属性（自动脱敏由日志侧负责）。
func (s *Span) SetAttr(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attrs[key] = value
}

// Attr 读取属性。
func (s *Span) Attr(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.attrs[key]
	return value, ok
}

// End 结束 span 并记录错误。
func (s *Span) End(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.EndedAt.IsZero() {
		return
	}
	s.EndedAt = time.Now().UTC()
	s.Err = err
}

// Duration 返回 span 时长；未结束时返回 0。
func (s *Span) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.EndedAt.IsZero() {
		return 0
	}
	return s.EndedAt.Sub(s.Start)
}

// Finished 表示 span 已结束。
func (s *Span) Finished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.EndedAt.IsZero()
}

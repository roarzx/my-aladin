package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	traceIDKey
)

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// RequestID 生成或透传 X-Request-ID。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// TraceID 生成或透传 X-Trace-ID（供后续 H2 的 trace 关联使用）。
func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Trace-ID")
		if id == "" {
			id = newID()
		}
		w.Header().Set("X-Trace-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceIDKey, id)))
	})
}

// RequestIDFrom 取出请求 ID。
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// TraceIDFrom 取出 trace ID。
func TraceIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

// Recover 捕获 panic，返回统一 500，不让进程退出。
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic",
						"request_id", RequestIDFrom(r.Context()),
						"path", r.URL.Path,
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					errcode.Write(w, errcode.New(errcode.InternalError, "internal error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate 解析主体并放入 context。
func Authenticate(a auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a == nil {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "认证未配置"))
				return
			}
			actor, err := a.Authenticate(r)
			if err != nil {
				errcode.Write(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(), actor)))
		})
	}
}

// MemberLookup 按工作区解析成员角色。
type MemberLookup interface {
	MemberRole(ctx context.Context, workspaceID string, userID uint64) (domain.Role, error)
}

// WorkspaceScope 从路径参数 {wid} 解析工作区并注入成员角色。
//
// 非成员返回 403；依赖不可用时返回 503。资源是否存在不在这里判断。
func WorkspaceScope(members MemberLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := auth.ActorFrom(r.Context())
			if !ok {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "未认证"))
				return
			}
			workspaceID := chi.URLParam(r, "wid")
			if workspaceID == "" {
				errcode.Write(w, errcode.New(errcode.ValidationFailed, "缺少工作区 ID"))
				return
			}
			if members == nil {
				errcode.Write(w, errcode.New(errcode.UpstreamUnavailable, "成员仓储不可用"))
				return
			}
			role, err := members.MemberRole(r.Context(), workspaceID, actor.UserID)
			if errors.Is(err, sql.ErrNoRows) {
				errcode.Write(w, errcode.New(errcode.Forbidden, "不是该工作区成员"))
				return
			}
			if err != nil {
				errcode.Write(w, errcode.New(errcode.UpstreamUnavailable, "查询成员角色失败"))
				return
			}
			actor.WorkspaceID = workspaceID
			actor.Role = role
			next.ServeHTTP(w, r.WithContext(auth.WithActor(r.Context(), actor)))
		})
	}
}

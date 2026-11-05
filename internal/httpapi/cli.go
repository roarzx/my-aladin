package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ContextVersion 是上下文版本列表项。
type ContextVersion struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

// ContextStore 是 CLI 上下文 API 的存储接口。
type ContextStore interface {
	ListVersions(ctx context.Context, taskID, scopeKind, scopeID string) ([]ContextVersion, error)
	GetVersion(ctx context.Context, taskID, scopeKind, scopeID, kind string, version int) (string, error)
	PutVersion(ctx context.Context, taskID, scopeKind, scopeID, kind, content string, expectedVersion int) (int, error)
}

type machineCtxKey struct{}

// MachineAuth 校验机器令牌并把 machine_id 放入 context（9.6.1）。
func MachineAuth(fn func(*http.Request) (string, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fn == nil {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "机器认证未配置"))
				return
			}
			machineID, err := fn(r)
			if err != nil {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "机器令牌无效"))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), machineCtxKey{}, machineID)))
		})
	}
}

// MachineIDFrom 取出已认证的机器 ID。
func MachineIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(machineCtxKey{}).(string)
	return id
}

// MountCLI 挂载 9.6.1 的上下文端点（需在外层套 MachineAuth）。
func MountCLI(r chi.Router, store ContextStore) {
	r.Route("/contexts/{taskID}/{scope}", func(r chi.Router) {
		r.Get("/files", func(w http.ResponseWriter, req *http.Request) {
			scopeID, ok := resolveScopeID(w, req)
			if !ok {
				return
			}
			versions, err := store.ListVersions(req.Context(),
				chi.URLParam(req, "taskID"), chi.URLParam(req, "scope"), scopeID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			Data(w, http.StatusOK, versions)
		})

		r.Get("/files/{kind}/{version}", func(w http.ResponseWriter, req *http.Request) {
			scopeID, ok := resolveScopeID(w, req)
			if !ok {
				return
			}
			version, err := strconv.Atoi(chi.URLParam(req, "version"))
			if err != nil || version < 1 {
				errcode.Write(w, errcode.New(errcode.ValidationFailed, "version 必须是正整数"))
				return
			}
			kind := chi.URLParam(req, "kind")
			content, err := store.GetVersion(req.Context(),
				chi.URLParam(req, "taskID"), chi.URLParam(req, "scope"), scopeID, kind, version)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			Data(w, http.StatusOK, map[string]any{"kind": kind, "version": version, "content": content})
		})

		r.Put("/files/{kind}", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				ScopeID         string `json:"scopeId"`
				Content         string `json:"content"`
				ExpectedVersion int    `json:"expectedVersion"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4<<20))
			if err := dec.Decode(&body); err != nil {
				errcode.Write(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
				return
			}
			scopeID := body.ScopeID
			if scopeID == "" {
				var ok bool
				scopeID, ok = resolveScopeID(w, req)
				if !ok {
					return
				}
			}
			version, err := store.PutVersion(req.Context(),
				chi.URLParam(req, "taskID"), chi.URLParam(req, "scope"), scopeID,
				chi.URLParam(req, "kind"), body.Content, body.ExpectedVersion)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			Data(w, http.StatusOK, map[string]any{"version": version})
		})
	})
}

func resolveScopeID(w http.ResponseWriter, r *http.Request) (string, bool) {
	scopeID := r.URL.Query().Get("scopeId")
	if scopeID == "" && chi.URLParam(r, "scope") == "task" {
		scopeID = chi.URLParam(r, "taskID")
	}
	if scopeID == "" {
		errcode.Write(w, errcode.New(errcode.ValidationFailed, "缺少 scopeId"))
		return "", false
	}
	return scopeID, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if e, ok := errcode.As(err); ok {
		errcode.Write(w, e)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		errcode.Write(w, errcode.New(errcode.NotFound, "上下文版本不存在"))
		return
	}
	errcode.Write(w, errcode.New(errcode.UpstreamUnavailable, "上下文存储不可用"))
}

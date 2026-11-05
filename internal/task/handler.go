package task

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

const maxBodyBytes = 1 << 20

// Creator 是 Handler 依赖的创建能力（便于测试替换）。
type Creator interface {
	Create(ctx context.Context, creatorID uint64, workspaceID string, req CreateRequest) (*domain.Task, error)
}

// Handler 提供 POST /workspaces/{wid}/issues。
type Handler struct {
	Service Creator
}

// Create 处理创建任务请求。
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return
	}
	if h.Service == nil {
		httpapi.Fail(w, errcode.New(errcode.InternalError, "服务未配置"))
		return
	}
	workspaceID := chi.URLParam(r, "wid")

	var req CreateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}

	task, err := h.Service.Create(r.Context(), actor.UserID, workspaceID, req)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusCreated, map[string]any{
		"id":     task.ID,
		"status": task.Status,
	})
}

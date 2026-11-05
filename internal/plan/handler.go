package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// MemberLookup 解析任务所属工作区的成员角色。
type MemberLookup interface {
	MemberRole(ctx context.Context, workspaceID string, userID uint64) (domain.Role, error)
}

// Handler 提供计划提交、确认与启动端点（D5/D6）。
type Handler struct {
	DB      *sql.DB
	Members MemberLookup
	Service *Service
}

// Submit 处理 POST /issues/{id}/plans。
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if _, _, err := h.authorize(r, taskID, auth.ActionApprove); err != nil {
		httpapi.Fail(w, err)
		return
	}

	var req Proposal
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}

	plan, steps, err := h.Service.Propose(r.Context(), taskID, req)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusCreated, map[string]any{
		"planId":   plan.ID,
		"revision": plan.Revision,
		"steps":    len(steps),
	})
}

// Confirm 处理 POST /plans/{id}/confirm。
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "id")
	plan, _, err := mysqlstore.GetPlanByID(r.Context(), h.DB, planID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "计划不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取计划失败"))
		return
	}
	task, actor, err := h.authorize(r, plan.TaskID, auth.ActionApprove)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	_ = task

	var body struct {
		Revision    int    `json:"revision"`
		ContentHash string `json:"contentHash"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}

	confirmation, err := h.Service.Confirm(r.Context(), planID, body.Revision, body.ContentHash, actor.UserID)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusOK, map[string]any{
		"revision":    confirmation.Revision,
		"confirmedAt": confirmation.CreatedAt,
	})
}

// Start 处理 POST /issues/{id}/start。
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if _, _, err := h.authorize(r, taskID, auth.ActionConfirmStart); err != nil {
		httpapi.Fail(w, err)
		return
	}

	var body struct {
		ExpectedVersion uint64 `json:"expectedVersion"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}

	task, err := h.Service.Start(r.Context(), taskID, body.ExpectedVersion)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusOK, map[string]any{
		"startedAt": time.Now().UTC(),
		"status":    task.Status,
	})
}

func (h *Handler) authorize(r *http.Request, taskID string, action auth.Action) (domain.Task, auth.Actor, error) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		return domain.Task{}, actor, errcode.New(errcode.Unauthenticated, "未认证")
	}

	task, err := mysqlstore.GetTask(r.Context(), h.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, actor, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.Task{}, actor, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}

	role, err := h.Members.MemberRole(r.Context(), task.WorkspaceID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		// 不泄漏任务存在性
		return domain.Task{}, actor, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.Task{}, actor, errcode.New(errcode.UpstreamUnavailable, "读取成员角色失败")
	}

	actor.WorkspaceID = task.WorkspaceID
	actor.Role = role
	if !auth.Can(actor, action, auth.Resource{
		WorkspaceID: task.WorkspaceID,
		TaskOwnerID: task.CreatedBy,
	}) {
		return domain.Task{}, actor, errcode.New(errcode.Forbidden, "无权限执行该操作")
	}
	return task, actor, nil
}

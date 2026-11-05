package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// MemberLookup 解析工作区成员角色。
type MemberLookup interface {
	MemberRole(ctx context.Context, workspaceID string, userID uint64) (domain.Role, error)
}

// Handler 提供 POST /steps/{id}/reconcile（人工裁决，E6）。
type Handler struct {
	DB         *sql.DB
	Members    MemberLookup
	Reconciler *Reconciler
}

// Reconcile 写入人工裁决并落状态。
func (h *Handler) Reconcile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stepID := chi.URLParam(r, "id")

	step, err := mysqlstore.GetStepByID(ctx, h.DB, stepID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "步骤不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取步骤失败"))
		return
	}
	plan, _, err := mysqlstore.GetPlanByID(ctx, h.DB, step.PlanID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取计划失败"))
		return
	}
	task, err := mysqlstore.GetTask(ctx, h.DB, plan.TaskID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务失败"))
		return
	}
	actor, ok := auth.ActorFrom(ctx)
	if !ok {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return
	}
	role, err := h.Members.MemberRole(ctx, task.WorkspaceID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取成员角色失败"))
		return
	}
	actor.WorkspaceID = task.WorkspaceID
	actor.Role = role
	if !auth.Can(actor, auth.ActionRetryStep, auth.Resource{
		WorkspaceID: task.WorkspaceID, TaskOwnerID: task.CreatedBy,
	}) {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "无权限裁决该步骤"))
		return
	}

	var body struct {
		Verdict  string `json:"verdict"`
		Evidence string `json:"evidence"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	verdict := Verdict(body.Verdict)
	if err := h.Reconciler.ApplyVerdict(ctx, stepID, verdict, body.Evidence); err != nil {
		httpapi.Fail(w, err)
		return
	}
	updated, err := mysqlstore.GetStepByID(ctx, h.DB, stepID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取步骤失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, map[string]any{
		"stepStatus": string(updated.Status),
		"verdict":    string(verdict),
	})
}

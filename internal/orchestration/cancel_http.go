package orchestration

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/httpapi"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// CancelHandler 提供 POST /issues/{id}/cancel（E9）。
type CancelHandler struct {
	Members   MemberLookup
	Canceller *Canceller
}

// Cancel 发起取消；所有执行都确认停止时立即落终态，否则保持 cancelling 等待超时。
func (h *CancelHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	taskID := chi.URLParam(r, "id")

	task, err := mysqlstore.GetTask(ctx, h.Canceller.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return
	}
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
	if !auth.Can(actor, auth.ActionCancelTask, auth.Resource{
		WorkspaceID: task.WorkspaceID, TaskOwnerID: task.CreatedBy,
	}) {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "无权限取消该任务"))
		return
	}

	var body struct {
		ExpectedVersion uint64 `json:"expectedVersion"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	if body.ExpectedVersion != 0 && body.ExpectedVersion != task.Version {
		httpapi.Fail(w, errcode.Newf(errcode.Conflict,
			"任务版本已变化: 当前 %d", task.Version))
		return
	}

	updated, allAcknowledged, err := h.Canceller.RequestCancel(ctx, taskID)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	if allAcknowledged {
		if err := h.Canceller.FinalizeCancel(ctx, taskID, false); err != nil {
			httpapi.Fail(w, err)
			return
		}
	}
	httpapi.Data(w, http.StatusOK, map[string]any{
		"cancelRequestedAt": updated.CancelRequestedAt,
		"status":            updated.Status,
	})
}

// Package activity 提供任务事件流端点（D13）。
package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

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

// Handler 提供 GET /issues/{id}/events?after=。
type Handler struct {
	DB      *sql.DB
	Members MemberLookup
}

// List 返回游标之后的事件与 nextSeq。
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	taskID := chi.URLParam(r, "id")

	task, err := mysqlstore.GetTask(ctx, h.DB, taskID)
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
	if !auth.Can(actor, auth.ActionView, auth.Resource{WorkspaceID: task.WorkspaceID, TaskOwnerID: task.CreatedBy}) {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "无权限查看该任务"))
		return
	}

	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	events, err := mysqlstore.ListActivities(ctx, h.DB, taskID, after, 200)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取事件失败"))
		return
	}
	nextSeq := after
	for _, event := range events {
		if event.Seq > nextSeq {
			nextSeq = event.Seq
		}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"data":    events,
		"nextSeq": nextSeq,
	})
}

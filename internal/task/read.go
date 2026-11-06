package task

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// MemberLookup 解析工作区成员角色（与 activity/plan 的读取权限口径一致）。
type MemberLookup interface {
	MemberRole(ctx context.Context, workspaceID string, userID uint64) (domain.Role, error)
}

type taskView struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Name        string     `json:"name"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Version     uint64     `json:"version"`
	PlanBranch  string     `json:"planBranch,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CancelAt    *time.Time `json:"cancelRequestedAt,omitempty"`
}

type planSummary struct {
	ID          string `json:"id"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	ContentHash string `json:"contentHash"`
	StepCount   int    `json:"stepCount"`
}

type agentSummary struct {
	Ref     string `json:"ref"`
	AgentID string `json:"agentId"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

type repositorySummary struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
}

// Get 处理 GET /issues/{id}：任务 + 计划摘要 + 验收摘要。
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
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
		// Workspace 隔离：非成员不暴露任务存在与否
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

	var plan *planSummary
	revision, err := mysqlstore.LatestPlanRevision(ctx, h.DB, taskID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取计划失败"))
		return
	}
	if revision > 0 {
		p, steps, err := mysqlstore.GetPlanByRevision(ctx, h.DB, taskID, revision)
		if err != nil {
			httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取计划失败"))
			return
		}
		plan = &planSummary{
			ID: p.ID, Revision: p.Revision, Status: string(p.Status),
			ContentHash: p.ContentHash, StepCount: len(steps),
		}
	}

	agents, err := mysqlstore.ListTaskAgents(ctx, h.DB, taskID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务成员失败"))
		return
	}
	repos, err := mysqlstore.ListTaskRepositories(ctx, h.DB, taskID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务仓库失败"))
		return
	}

	agentViews := make([]agentSummary, 0, len(agents))
	for _, a := range agents {
		agentViews = append(agentViews, agentSummary{
			Ref: a.Ref, AgentID: a.AgentID, Name: a.Name, Role: a.Role,
		})
	}
	repoViews := make([]repositorySummary, 0, len(repos))
	for _, repo := range repos {
		repoViews = append(repoViews, repositorySummary{
			ID: repo.RepositoryID, URL: repo.URL, DefaultBranch: repo.DefaultBranch,
		})
	}

	httpapi.Data(w, http.StatusOK, map[string]any{
		"task": taskView{
			ID: task.ID, WorkspaceID: task.WorkspaceID, Name: task.Name,
			Mode: string(task.Mode), Status: string(task.Status), Version: task.Version,
			PlanBranch: task.PlanBranch, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
			CancelAt: task.CancelRequestedAt,
		},
		"plan":         plan,
		"acceptance":   nil,
		"agents":       agentViews,
		"repositories": repoViews,
	})
}

// List 处理 GET /issues：列出当前用户可见的任务（驾驶舱首页）。
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tasks, err := mysqlstore.ListTasksForUser(r.Context(), h.DB, actor.UserID, limit)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务列表失败"))
		return
	}
	out := make([]taskView, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskView{
			ID: task.ID, WorkspaceID: task.WorkspaceID, Name: task.Name,
			Mode: string(task.Mode), Status: string(task.Status), Version: task.Version,
			PlanBranch: task.PlanBranch, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
			CancelAt: task.CancelRequestedAt,
		})
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"data": out})
}

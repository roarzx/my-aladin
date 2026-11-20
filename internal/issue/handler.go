package issue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
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

// Handler 提供 Issue 的执行入口：开始、发消息、取消、查询执行记录。
type Handler struct {
	DB      *sql.DB
	Members MemberLookup
	Service *Service
}

// Start 处理 POST /issues/{id}/start：开始或继续一轮执行。
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	task, _, err := h.authorize(w, r, auth.ActionConfirmStart)
	if err != nil {
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	// 允许空 body：使用 Issue 自带的需求作为提示词。
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil &&
		!errors.Is(err, io.EOF) {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}

	prompt := body.Prompt
	if prompt == "" {
		prompt = defaultPrompt(task)
	}
	run, err := h.Service.StartTurn(r.Context(), task.ID, prompt)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusAccepted, run)
}

// Message 处理 POST /issues/{id}/messages：把用户消息交给 Agent。
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	task, _, err := h.authorize(w, r, auth.ActionApprove)
	if err != nil {
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	run, err := h.Service.Message(r.Context(), task.ID, body.Content)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusAccepted, run)
}

// Cancel 处理 POST /issues/{id}/cancel。
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	task, actor, err := h.authorize(w, r, auth.ActionApprove)
	if err != nil {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	reason := body.Reason
	if reason == "" {
		reason = "用户取消"
	}
	if err := h.Service.Cancel(r.Context(), task.ID, reason); err != nil {
		httpapi.Fail(w, err)
		return
	}
	_ = actor
	httpapi.Data(w, http.StatusOK, map[string]string{"status": string(domain.TaskCancelled)})
}

// Runs 处理 GET /issues/{id}/runs：最近的执行记录。
func (h *Handler) Runs(w http.ResponseWriter, r *http.Request) {
	task, _, err := h.authorize(w, r, auth.ActionView)
	if err != nil {
		return
	}
	runs, err := mysqlstore.ListIssueRuns(r.Context(), h.DB, task.ID, 50)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取执行记录失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, runs)
}

// authorize 校验成员身份与操作权限。
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action auth.Action) (domain.Task, auth.Actor, error) {
	ctx := r.Context()
	task, err := mysqlstore.GetTask(ctx, h.DB, chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, auth.Actor{}, err
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务失败"))
		return domain.Task{}, auth.Actor{}, err
	}
	actor, ok := auth.ActorFrom(ctx)
	if !ok {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return domain.Task{}, auth.Actor{}, errors.New("未认证")
	}
	role, err := h.Members.MemberRole(ctx, task.WorkspaceID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, auth.Actor{}, err
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取成员角色失败"))
		return domain.Task{}, auth.Actor{}, err
	}
	actor.WorkspaceID = task.WorkspaceID
	actor.Role = role
	if !auth.Can(actor, action, auth.Resource{WorkspaceID: task.WorkspaceID, TaskOwnerID: task.CreatedBy}) {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "无权限操作该任务"))
		return domain.Task{}, auth.Actor{}, errors.New("无权限")
	}
	return task, actor, nil
}

// defaultPrompt 在没有显式提示词时，用需求正文（或标题）开始第一轮。
func defaultPrompt(task domain.Task) string {
	prompt := task.Requirement
	if prompt == "" {
		prompt = task.Name
	}
	if task.InformDoc != "" {
		prompt += "\n\n补充说明：\n" + task.InformDoc
	}
	return prompt
}

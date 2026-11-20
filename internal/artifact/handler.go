package artifact

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

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

// Handler 提供产物上传（机器令牌）、列表与下载（用户令牌）。
type Handler struct {
	Service *Service
	DB      *sql.DB
	Members MemberLookup
}

// Upload 处理 POST /api/v1/cli/artifacts。
//
// daemon 在步骤成功后把新增/修改的文件逐个上传；服务端以 execution_id 反查
// attempt，再推导出 task/step，避免客户端伪造归属。
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExecutionID string `json:"execution_id"`
		Path        string `json:"path"`
		ContentType string `json:"content_type"`
		Content     string `json:"content_base64"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*MaxFileBytes))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	if strings.TrimSpace(body.ExecutionID) == "" || strings.TrimSpace(body.Path) == "" {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "缺少 execution_id 或 path"))
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Content)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "content_base64 不合法"))
		return
	}

	run, err := mysqlstore.GetIssueRunByExecution(r.Context(), h.DB, body.ExecutionID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "执行不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取执行失败"))
		return
	}

	item, err := h.Service.Save(r.Context(), run.TaskID, run.ID,
		safeDisplayPath(body.Path), body.ContentType, data)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusCreated, item)
}

// List 处理 GET /issues/{id}/artifacts。
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	task, ok := h.authorize(w, r)
	if !ok {
		return
	}
	items, err := h.Service.List(r.Context(), task.ID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取产物失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, items)
}

// Download 处理 GET /issues/{id}/artifacts/{artifactId}/content。
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	task, ok := h.authorize(w, r)
	if !ok {
		return
	}
	item, file, err := h.Service.Open(r.Context(), chi.URLParam(r, "artifactId"))
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	defer func() { _ = file.Close() }()
	if item.TaskID != task.ID {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "产物不存在"))
		return
	}
	contentType := item.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(item.SizeBytes, 10))
	w.Header().Set("Content-Disposition",
		`attachment; filename*=UTF-8''`+url.PathEscape(displayName(item.Path)))
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 64<<10)
	if _, err := io.CopyBuffer(w, file, buf); err != nil {
		h.Service.log().Warn("发送产物失败", "artifact", item.ID, "err", err)
	}
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) (domain.Task, bool) {
	ctx := r.Context()
	taskID := chi.URLParam(r, "id")
	task, err := mysqlstore.GetTask(ctx, h.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, false
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务失败"))
		return domain.Task{}, false
	}
	actor, ok := auth.ActorFrom(ctx)
	if !ok {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return domain.Task{}, false
	}
	role, err := h.Members.MemberRole(ctx, task.WorkspaceID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, false
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取成员角色失败"))
		return domain.Task{}, false
	}
	actor.WorkspaceID = task.WorkspaceID
	actor.Role = role
	if !auth.Can(actor, auth.ActionView, auth.Resource{WorkspaceID: task.WorkspaceID, TaskOwnerID: task.CreatedBy}) {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "无权限查看该任务"))
		return domain.Task{}, false
	}
	return task, true
}

// safeDisplayPath 只保留相对路径形态，拒绝绝对路径与 ../ 逃逸。
func safeDisplayPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	cleaned := path.Clean(p)
	cleaned = strings.TrimPrefix(cleaned, "/")
	for strings.HasPrefix(cleaned, "../") {
		cleaned = strings.TrimPrefix(cleaned, "../")
	}
	if cleaned == "" || cleaned == "." {
		return "artifact"
	}
	if len(cleaned) > 480 {
		cleaned = cleaned[len(cleaned)-480:]
	}
	return cleaned
}

func displayName(p string) string {
	if name := path.Base(p); name != "" && name != "." && name != "/" {
		return name
	}
	return "artifact"
}

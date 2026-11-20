package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// CLIHandler 提供 Agent 侧的 Issue 接口（机器令牌）。
//
// 对应 `aladdin openapi issue ...` 的五个命令：get / list / create /
// update-status / input request。与面向用户的 Handler 不同，这里没有用户
// 会话，身份来自机器令牌，归属校验口径是：
//   - 读：Issue 与机器同属一个工作区；
//   - 写：该 Issue 还分配了运行在这台机器上的 Agent。
//
// 状态由 Agent 驱动：update-status 直接落在 tasks.status 上，不经过平台
// 状态机（见 domain.Task.ReportStatus）。
type CLIHandler struct {
	DB      *sql.DB
	Service Creator
}

type cliAgentView struct {
	Ref     string `json:"ref"`
	AgentID string `json:"agentId"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

type cliIssueView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Mode        string         `json:"mode"`
	Version     uint64         `json:"version"`
	Requirement string         `json:"requirement,omitempty"`
	InformDoc   string         `json:"informDoc,omitempty"`
	PlanBranch  string         `json:"planBranch,omitempty"`
	Agents      []cliAgentView `json:"agents,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

func newCLIIssueView(t domain.Task, agents []mysqlstore.TaskAgent) cliIssueView {
	view := cliIssueView{
		ID: t.ID, Name: t.Name, Status: displayStatus(t.Status), Mode: string(t.Mode),
		Version: t.Version, Requirement: t.Requirement, InformDoc: t.InformDoc,
		PlanBranch: t.PlanBranch, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	for _, a := range agents {
		view.Agents = append(view.Agents, cliAgentView{
			Ref: a.Ref, AgentID: a.AgentID, Name: a.Name, Role: a.Role,
		})
	}
	return view
}

// Get 处理 GET /api/v1/cli/issues/{id}。
func (h *CLIHandler) Get(w http.ResponseWriter, r *http.Request) {
	m, ok := h.machine(w, r)
	if !ok {
		return
	}
	task, ok := h.loadIssue(w, r, m)
	if !ok {
		return
	}
	agents, err := mysqlstore.ListTaskAgents(r.Context(), h.DB, task.ID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务成员失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, newCLIIssueView(task, agents))
}

// List 处理 GET /api/v1/cli/issues。
func (h *CLIHandler) List(w http.ResponseWriter, r *http.Request) {
	m, ok := h.machine(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()

	filter := mysqlstore.TaskFilter{Keyword: strings.TrimSpace(query.Get("keyword"))}
	limit, _ := strconv.Atoi(query.Get("limit"))
	filter.Limit = limit
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		status, err := domain.ParseReportedStatus(raw)
		if err != nil {
			httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "status 取值非法: "+raw))
			return
		}
		filter.Status = status
	}
	for _, item := range []struct {
		name  string
		apply func(time.Time)
	}{
		{"created-at-start", func(v time.Time) { filter.CreatedFrom = v }},
		{"created-at-end", func(v time.Time) { filter.CreatedTo = v }},
		{"updated-at-start", func(v time.Time) { filter.UpdatedFrom = v }},
		{"updated-at-end", func(v time.Time) { filter.UpdatedTo = v }},
	} {
		raw := strings.TrimSpace(query.Get(item.name))
		if raw == "" {
			continue
		}
		parsed, err := parseTimeParam(raw, strings.HasSuffix(item.name, "-end"))
		if err != nil {
			httpapi.Fail(w, errcode.Newf(errcode.ValidationFailed, "%s 时间格式不合法: %s", item.name, raw))
			return
		}
		item.apply(parsed)
	}

	tasks, err := mysqlstore.ListTasksInWorkspace(r.Context(), h.DB, m.WorkspaceID, filter)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务列表失败"))
		return
	}
	out := make([]cliIssueView, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, newCLIIssueView(t, nil))
	}
	httpapi.Data(w, http.StatusOK, out)
}

type cliCreateRequest struct {
	Name           string   `json:"name"`
	AgentID        string   `json:"agentId"`
	Requirement    string   `json:"requirementContent"`
	InformDoc      string   `json:"informDoc"`
	RepositoryURL  string   `json:"repositoryUrl"`
	IdempotencyKey string   `json:"idempotencyKey"`
	Mode           string   `json:"mode"`
	PlannerAgentID string   `json:"plannerAgentId"`
	TemplateID     string   `json:"templateId"`
	RepositoryIDs  []string `json:"repositoryIds"`
	AgentIDs       []string `json:"agentIds"`
	SquadID        string   `json:"squadId"`
}

// Create 处理 POST /api/v1/cli/issues：Agent 用 CLI 建 Issue。
func (h *CLIHandler) Create(w http.ResponseWriter, r *http.Request) {
	m, ok := h.machine(w, r)
	if !ok {
		return
	}
	var body cliCreateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	body.AgentID = strings.TrimSpace(body.AgentID)
	if body.AgentID == "" {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "agentId 必填"))
		return
	}

	if key := strings.TrimSpace(body.IdempotencyKey); key != "" {
		existing, err := mysqlstore.FindTaskByIdempotencyKey(r.Context(), h.DB, m.WorkspaceID, key)
		if err == nil {
			task, getErr := mysqlstore.GetTask(r.Context(), h.DB, existing)
			if getErr != nil {
				httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务失败"))
				return
			}
			httpapi.Data(w, http.StatusOK, newCLIIssueView(task, nil))
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "幂等键查询失败"))
			return
		}
	}

	owner, err := mysqlstore.GetAgent(r.Context(), h.DB, body.AgentID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "Agent 不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败"))
		return
	}
	if owner.WorkspaceID != m.WorkspaceID {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "Agent 不属于机器所在工作区"))
		return
	}

	req := CreateRequest{
		Name:           strings.TrimSpace(body.Name),
		Requirement:    body.Requirement,
		InformDoc:      body.InformDoc,
		Mode:           domain.TaskMode(strings.TrimSpace(body.Mode)),
		TemplateID:     body.TemplateID,
		AgentIDs:       body.AgentIDs,
		PlannerAgentID: body.PlannerAgentID,
		SquadID:        body.SquadID,
		Repositories:   body.RepositoryIDs,
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
	}
	// CLI 默认「一个 Issue 一个 Agent」：mode/agentIds 未显式给出时自动补齐。
	if req.Mode == "" {
		req.Mode = domain.ModeOrdinary
	}
	if req.Mode == domain.ModeOrdinary && len(req.AgentIDs) == 0 {
		req.AgentIDs = []string{body.AgentID}
	}
	if req.Mode == domain.ModeOrdinary && req.PlannerAgentID == "" {
		req.PlannerAgentID = body.AgentID
	}
	if url := strings.TrimSpace(body.RepositoryURL); url != "" {
		repoID, err := mysqlstore.FindRepositoryByURL(r.Context(), h.DB, m.WorkspaceID, url)
		if errors.Is(err, sql.ErrNoRows) {
			httpapi.Fail(w, errcode.Newf(errcode.ValidationFailed,
				"仓库未在工作区登记: %s", url))
			return
		}
		if err != nil {
			httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "查找仓库失败"))
			return
		}
		req.Repositories = append(req.Repositories, repoID)
	}
	if h.Service == nil {
		httpapi.Fail(w, errcode.New(errcode.InternalError, "服务未配置"))
		return
	}

	task, err := h.Service.Create(r.Context(), owner.OwnerID, m.WorkspaceID, req)
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusCreated, newCLIIssueView(*task, nil))
}

// UpdateStatus 处理 POST /api/v1/cli/issues/{id}/status：Agent 上报状态。
func (h *CLIHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	m, ok := h.machine(w, r)
	if !ok {
		return
	}
	task, ok := h.loadIssue(w, r, m)
	if !ok {
		return
	}
	if ok := h.authorizeWriter(w, r, m, task.ID); !ok {
		return
	}

	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	next, err := domain.ParseReportedStatus(body.Status)
	if err != nil {
		httpapi.Fail(w, errcode.Newf(errcode.ValidationFailed, "status 取值非法: %s", body.Status))
		return
	}

	from, version := task.Status, task.Version
	changed := next != from
	if err := task.ReportStatus(next); err != nil {
		httpapi.Fail(w, errcode.New(errcode.Conflict, err.Error()))
		return
	}
	if changed {
		if _, err := mysqlstore.UpdateTaskState(r.Context(), h.DB,
			task.ID, from, next, version); err != nil {
			httpapi.Fail(w, err)
			return
		}
		_, _ = mysqlstore.AppendActivity(r.Context(), h.DB, task.ID, "issue.status", map[string]any{
			"from":   string(from),
			"to":     string(next),
			"reason": truncateCLI(body.Reason, 2000),
			"source": "agent",
		})
	}
	httpapi.Data(w, http.StatusOK, newCLIIssueView(task, nil))
}

// InputRequest 处理 POST /api/v1/cli/issues/{id}/input-requests。
//
// 结构化输入请求只落成一条活动（issue.input_requested），供会话页展示；
// 平台不因此改状态——是否等待由 Agent 自己用 update-status 表达。
func (h *CLIHandler) InputRequest(w http.ResponseWriter, r *http.Request) {
	m, ok := h.machine(w, r)
	if !ok {
		return
	}
	task, ok := h.loadIssue(w, r, m)
	if !ok {
		return
	}
	if ok := h.authorizeWriter(w, r, m, task.ID); !ok {
		return
	}

	var body struct {
		Reason  string   `json:"reason"`
		Message string   `json:"message"`
		Options []string `json:"options"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "message 必填"))
		return
	}

	activity, err := mysqlstore.AppendActivity(r.Context(), h.DB, task.ID, "issue.input_requested",
		map[string]any{
			"reason":  truncateCLI(body.Reason, 200),
			"message": truncateCLI(body.Message, 8000),
			"options": body.Options,
			"status":  "pending",
		})
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "写入输入请求失败"))
		return
	}
	httpapi.Data(w, http.StatusCreated, map[string]any{
		"id": activity.EventID, "issueId": task.ID, "status": "pending",
	})
}

// machine 解析机器令牌对应的机器。
func (h *CLIHandler) machine(w http.ResponseWriter, r *http.Request) (machine.Machine, bool) {
	machineID := httpapi.MachineIDFrom(r.Context())
	if machineID == "" {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "未认证的机器"))
		return machine.Machine{}, false
	}
	m, err := mysqlstore.GetMachine(r.Context(), h.DB, machineID)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.Unauthenticated, "机器不存在"))
		return machine.Machine{}, false
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取机器失败"))
		return machine.Machine{}, false
	}
	return m, true
}

// loadIssue 读取 Issue 并校验它属于机器所在工作区。
func (h *CLIHandler) loadIssue(w http.ResponseWriter, r *http.Request, m machine.Machine) (domain.Task, bool) {
	task, err := mysqlstore.GetTask(r.Context(), h.DB, chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, false
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取任务失败"))
		return domain.Task{}, false
	}
	if task.WorkspaceID != m.WorkspaceID {
		httpapi.Fail(w, errcode.New(errcode.NotFound, "任务不存在"))
		return domain.Task{}, false
	}
	return task, true
}

// authorizeWriter 要求该 Issue 分配了运行在本机的 Agent。
func (h *CLIHandler) authorizeWriter(w http.ResponseWriter, r *http.Request, m machine.Machine, taskID string) bool {
	bound, err := mysqlstore.TaskAgentOnMachine(r.Context(), h.DB, taskID, m.ID)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "校验机器与任务的绑定失败"))
		return false
	}
	if !bound {
		httpapi.Fail(w, errcode.New(errcode.Forbidden, "本机没有该 Issue 的 Agent，不能上报"))
		return false
	}
	return true
}

// parseTimeParam 解析 CLI 传入的时间：日期、日期时间或 RFC3339。
// end 为真且只给了日期时，取当天 23:59:59。
func parseTimeParam(raw string, end bool) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, raw, time.Local)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" && end {
			parsed = parsed.Add(24*time.Hour - time.Second)
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("时间格式不合法")
}

// displayStatus 把库内状态名换成 Agent 面向的名字，输出与入参保持同一套词汇。
func displayStatus(status domain.TaskStatus) string {
	switch status {
	case domain.TaskExecuting:
		return "in_progress"
	case domain.TaskPaused:
		return "blocked"
	}
	return string(status)
}

func truncateCLI(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

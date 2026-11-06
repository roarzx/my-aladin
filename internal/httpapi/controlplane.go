package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// machineTokenTTL 是签发机器密钥的有效期。
const machineTokenTTL = 30 * 24 * time.Hour

// controlPlaneMaxBody 是控制面请求体的上限。
const controlPlaneMaxBody = 1 << 20

// ControlPlane 提供工作区、机器、Agent 与仓库的配置端点。
// 这些路由的 workspaceId 来自请求体或查询串，因此不走 WorkspaceScope，
// 由各处理器显式校验成员关系。
type ControlPlane struct {
	DB      *sql.DB
	Members MemberLookup
}

// Mount 注册控制面路由（需挂在已认证的全局分组上）。
func (h *ControlPlane) Mount(r chi.Router) {
	r.Get("/workspaces", h.listWorkspaces)

	r.Get("/machines", h.listMachines)
	r.Post("/machines", h.createMachine)
	r.Post("/machines/{id}/token", h.issueMachineToken)

	r.Get("/agents", h.listAgents)
	r.Post("/agents", h.createAgent)
	r.Patch("/agents/{id}", h.patchAgent)
	r.Delete("/agents/{id}", h.deleteAgent)

	r.Get("/repositories", h.listRepositories)
	r.Post("/repositories", h.createRepository)
}

// ---------- 视图 ----------

type machineView struct {
	ID           string               `json:"id"`
	WorkspaceID  string               `json:"workspaceId"`
	Name         string               `json:"name"`
	Kind         string               `json:"kind"`
	Status       string               `json:"status"`
	Capabilities []machine.Capability `json:"capabilities"`
	CLIVersion   string               `json:"cliVersion"`
	LastSeenAt   *time.Time           `json:"lastSeenAt"`
}

func newMachineView(m machine.Machine) machineView {
	caps := m.Capabilities
	if caps == nil {
		caps = []machine.Capability{}
	}
	return machineView{
		ID:           m.ID,
		WorkspaceID:  m.WorkspaceID,
		Name:         m.Name,
		Kind:         string(m.Kind),
		Status:       string(m.Status),
		Capabilities: caps,
		CLIVersion:   m.CLIVersion,
		LastSeenAt:   m.LastSeenAt,
	}
}

type agentView struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspaceId"`
	Name          string          `json:"name"`
	OwnerID       uint64          `json:"ownerId"`
	Visibility    string          `json:"visibility"`
	RuntimeType   string          `json:"runtimeType"`
	MachineID     string          `json:"machineId"`
	WorkDir       string          `json:"workDir"`
	Model         string          `json:"model"`
	ThinkingLevel string          `json:"thinkingLevel"`
	SystemPrompt  string          `json:"systemPrompt"`
	CapabilitySet json.RawMessage `json:"capabilitySet"`
	EnvVars       json.RawMessage `json:"envVars"`
	Enabled       bool            `json:"enabled"`
}

func newAgentView(a agent.Agent) agentView {
	return agentView{
		ID:            a.ID,
		WorkspaceID:   a.WorkspaceID,
		Name:          a.Name,
		OwnerID:       a.OwnerID,
		Visibility:    string(a.Visibility),
		RuntimeType:   string(a.RuntimeType),
		MachineID:     a.MachineID,
		WorkDir:       a.WorkDir,
		Model:         a.Model,
		ThinkingLevel: a.ThinkingLevel,
		SystemPrompt:  a.SystemPrompt,
		CapabilitySet: jsonOrEmptyObject(a.CapabilitySet),
		EnvVars:       jsonOrEmptyObject(a.EnvVars),
		Enabled:       a.Enabled,
	}
}

type repositoryView struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
	Provider      string `json:"provider"`
}

func newRepositoryView(r mysqlstore.Repository) repositoryView {
	return repositoryView{
		ID:            r.ID,
		WorkspaceID:   r.WorkspaceID,
		URL:           r.URL,
		DefaultBranch: r.DefaultBranch,
		Provider:      r.Provider,
	}
}

// ---------- 工作区 ----------

// listWorkspaces 处理 GET /workspaces。
func (h *ControlPlane) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return
	}
	memberships, err := mysqlstore.ListWorkspacesForUser(r.Context(), h.DB, actor.UserID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取工作区失败"))
		return
	}
	out := make([]map[string]any, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, map[string]any{"id": m.ID, "name": m.Name, "role": string(m.Role)})
	}
	Data(w, http.StatusOK, out)
}

// authorize 校验当前用户是 workspaceID 的成员，并判断其是否具备 action 权限。
func (h *ControlPlane) authorize(w http.ResponseWriter, r *http.Request, workspaceID string, action auth.Action) (auth.Actor, bool) {
	if strings.TrimSpace(workspaceID) == "" {
		Fail(w, errcode.New(errcode.ValidationFailed, "缺少 workspaceId"))
		return auth.Actor{}, false
	}
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		Fail(w, errcode.New(errcode.Unauthenticated, "未认证"))
		return auth.Actor{}, false
	}
	role, err := h.Members.MemberRole(r.Context(), workspaceID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, errcode.New(errcode.Forbidden, "无权访问该工作区"))
		return auth.Actor{}, false
	}
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取成员角色失败"))
		return auth.Actor{}, false
	}
	actor.WorkspaceID = workspaceID
	actor.Role = role
	if !auth.Can(actor, action, auth.Resource{WorkspaceID: workspaceID}) {
		Fail(w, errcode.New(errcode.Forbidden, "无权限执行该操作"))
		return auth.Actor{}, false
	}
	return actor, true
}

// decodeBody 解码 JSON 请求体，拒绝未知字段。
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, controlPlaneMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return false
	}
	return true
}

// ---------- 机器 ----------

// listMachines 处理 GET /machines?workspaceId=。
func (h *ControlPlane) listMachines(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if _, ok := h.authorize(w, r, workspaceID, auth.ActionView); !ok {
		return
	}
	machines, err := mysqlstore.ListMachinesByWorkspace(r.Context(), h.DB, workspaceID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取机器失败"))
		return
	}
	out := make([]machineView, 0, len(machines))
	for _, m := range machines {
		out = append(out, newMachineView(m))
	}
	Data(w, http.StatusOK, out)
}

// createMachine 处理 POST /machines，返回机器视图与一次性明文密钥。
func (h *ControlPlane) createMachine(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceID string `json:"workspaceId"`
		Name        string `json:"name"`
		Kind        string `json:"kind"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	actor, ok := h.authorize(w, r, body.WorkspaceID, auth.ActionCreateIssue)
	if !ok {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		Fail(w, errcode.New(errcode.ValidationFailed, "机器名称不能为空"))
		return
	}
	if len(name) > 128 {
		Fail(w, errcode.New(errcode.ValidationFailed, "机器名称过长"))
		return
	}
	kind := machine.KindLocal
	switch machine.Kind(body.Kind) {
	case "":
	case machine.KindLocal, machine.KindCloud:
		kind = machine.Kind(body.Kind)
	default:
		Fail(w, errcode.New(errcode.ValidationFailed, "机器类型不合法"))
		return
	}

	m := machine.Machine{
		ID:          idgen.New(),
		WorkspaceID: actor.WorkspaceID,
		Name:        name,
		Kind:        kind,
		Status:      machine.StatusOffline,
	}
	if err := mysqlstore.CreateMachine(r.Context(), h.DB, m); err != nil {
		if errors.Is(err, mysqlstore.ErrMachineNameTaken) {
			Fail(w, errcode.New(errcode.Conflict, "机器名称已存在"))
			return
		}
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "创建机器失败"))
		return
	}

	token, expiresAt, err := h.issueToken(r.Context(), m.ID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "签发机器密钥失败"))
		return
	}
	Data(w, http.StatusCreated, map[string]any{
		"machine":   newMachineView(m),
		"token":     token,
		"expiresAt": expiresAt,
	})
}

// issueMachineToken 处理 POST /machines/{id}/token。
func (h *ControlPlane) issueMachineToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, err := mysqlstore.GetMachine(r.Context(), h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, errcode.New(errcode.NotFound, "机器不存在"))
		return
	}
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取机器失败"))
		return
	}
	if _, ok := h.authorize(w, r, m.WorkspaceID, auth.ActionCreateIssue); !ok {
		return
	}
	token, expiresAt, err := h.issueToken(r.Context(), m.ID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "签发机器密钥失败"))
		return
	}
	Data(w, http.StatusOK, map[string]any{"token": token, "expiresAt": expiresAt})
}

// issueToken 生成明文密钥，仅把哈希写入 machine_tokens。
func (h *ControlPlane) issueToken(ctx context.Context, machineID string) (string, time.Time, error) {
	token, err := machine.NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(machineTokenTTL)
	if err := mysqlstore.CreateMachineToken(ctx, h.DB, machineID,
		machine.HashToken(token), expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// ---------- Agent ----------

// listAgents 处理 GET /agents?workspaceId=。
func (h *ControlPlane) listAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	actor, ok := h.authorize(w, r, workspaceID, auth.ActionView)
	if !ok {
		return
	}
	agents, err := mysqlstore.ListAgentsByWorkspace(r.Context(), h.DB, workspaceID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败"))
		return
	}
	manage := auth.Can(actor, auth.ActionManageCapabilities, auth.Resource{WorkspaceID: workspaceID})
	out := make([]agentView, 0, len(agents))
	for _, a := range agents {
		if !manage && !a.VisibleTo(actor.UserID) {
			continue
		}
		out = append(out, newAgentView(a))
	}
	Data(w, http.StatusOK, out)
}

// agentBody 是 POST/PATCH /agents 的请求体；PATCH 用指针区分「未提供」。
type agentBody struct {
	WorkspaceID   string           `json:"workspaceId"`
	Name          *string          `json:"name"`
	MachineID     *string          `json:"machineId"`
	WorkDir       *string          `json:"workDir"`
	SystemPrompt  *string          `json:"systemPrompt"`
	Visibility    *string          `json:"visibility"`
	RuntimeType   *string          `json:"runtimeType"`
	Model         *string          `json:"model"`
	ThinkingLevel *string          `json:"thinkingLevel"`
	CapabilitySet *json.RawMessage `json:"capabilitySet"`
	EnvVars       *json.RawMessage `json:"envVars"`
	Enabled       *bool            `json:"enabled"`
}

// createAgent 处理 POST /agents。
func (h *ControlPlane) createAgent(w http.ResponseWriter, r *http.Request) {
	var body agentBody
	if !decodeBody(w, r, &body) {
		return
	}
	actor, ok := h.authorize(w, r, body.WorkspaceID, auth.ActionCreateIssue)
	if !ok {
		return
	}

	a := agent.Agent{
		ID:          idgen.New(),
		WorkspaceID: actor.WorkspaceID,
		OwnerID:     actor.UserID,
		Visibility:  agent.Private,
		RuntimeType: agent.RuntimeClaudeCode,
		Enabled:     true,
	}
	if body.Name == nil {
		Fail(w, errcode.New(errcode.ValidationFailed, "Agent 名称不能为空"))
		return
	}
	a.Name = strings.TrimSpace(*body.Name)
	if a.Name == "" || len(a.Name) > 128 {
		Fail(w, errcode.New(errcode.ValidationFailed, "Agent 名称不能为空且不超过 128 字符"))
		return
	}
	if body.MachineID == nil || strings.TrimSpace(*body.MachineID) == "" {
		Fail(w, errcode.New(errcode.ValidationFailed, "必须指定机器"))
		return
	}
	a.MachineID = *body.MachineID
	if body.WorkDir == nil || !validWorkDir(*body.WorkDir) {
		Fail(w, errcode.New(errcode.ValidationFailed, "workDir 必须是绝对路径"))
		return
	}
	a.WorkDir = strings.TrimSpace(*body.WorkDir)
	if body.SystemPrompt != nil {
		a.SystemPrompt = *body.SystemPrompt
	}
	if body.Visibility != nil {
		a.Visibility = agent.Visibility(*body.Visibility)
	}
	if err := a.Visibility.Valid(); err != nil {
		Fail(w, errcode.New(errcode.ValidationFailed, "可见性取值不合法"))
		return
	}
	if body.RuntimeType != nil {
		if agent.RuntimeType(*body.RuntimeType) != agent.RuntimeClaudeCode {
			Fail(w, errcode.New(errcode.ValidationFailed, "暂不支持该运行时类型"))
			return
		}
		a.RuntimeType = agent.RuntimeClaudeCode
	}
	if body.Model != nil {
		a.Model = *body.Model
	}
	if body.ThinkingLevel != nil {
		a.ThinkingLevel = *body.ThinkingLevel
	}
	if body.Enabled != nil {
		a.Enabled = *body.Enabled
	}
	var err error
	if a.CapabilitySet, err = normalizeJSONObject(body.CapabilitySet); err != nil {
		Fail(w, errcode.New(errcode.ValidationFailed, "capabilitySet 必须是 JSON 对象"))
		return
	}
	if a.EnvVars, err = normalizeJSONObject(body.EnvVars); err != nil {
		Fail(w, errcode.New(errcode.ValidationFailed, "envVars 必须是 JSON 对象"))
		return
	}
	if !h.machineInWorkspace(w, r, a.MachineID, actor.WorkspaceID) {
		return
	}

	if err := mysqlstore.CreateAgent(r.Context(), h.DB, a); err != nil {
		if errors.Is(err, mysqlstore.ErrAgentNameTaken) {
			Fail(w, errcode.New(errcode.Conflict, "Agent 名称已存在"))
			return
		}
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "创建 Agent 失败"))
		return
	}
	Data(w, http.StatusCreated, newAgentView(a))
}

// patchAgent 处理 PATCH /agents/{id}，只覆盖请求中出现的字段。
func (h *ControlPlane) patchAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := mysqlstore.GetAgent(r.Context(), h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, errcode.New(errcode.NotFound, "Agent 不存在"))
		return
	}
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败"))
		return
	}
	actor, ok := h.authorize(w, r, current.WorkspaceID, auth.ActionCreateIssue)
	if !ok {
		return
	}
	if current.OwnerID != actor.UserID &&
		!auth.Can(actor, auth.ActionManageCapabilities, auth.Resource{WorkspaceID: current.WorkspaceID}) {
		Fail(w, errcode.New(errcode.Forbidden, "无权限修改该 Agent"))
		return
	}

	var body agentBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.WorkspaceID != "" && body.WorkspaceID != current.WorkspaceID {
		Fail(w, errcode.New(errcode.ValidationFailed, "不能跨工作区修改 Agent"))
		return
	}
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" || len(name) > 128 {
			Fail(w, errcode.New(errcode.ValidationFailed, "Agent 名称不能为空且不超过 128 字符"))
			return
		}
		current.Name = name
	}
	if body.MachineID != nil {
		if strings.TrimSpace(*body.MachineID) == "" {
			Fail(w, errcode.New(errcode.ValidationFailed, "必须指定机器"))
			return
		}
		if !h.machineInWorkspace(w, r, *body.MachineID, current.WorkspaceID) {
			return
		}
		current.MachineID = *body.MachineID
	}
	if body.WorkDir != nil {
		if !validWorkDir(*body.WorkDir) {
			Fail(w, errcode.New(errcode.ValidationFailed, "workDir 必须是绝对路径"))
			return
		}
		current.WorkDir = strings.TrimSpace(*body.WorkDir)
	}
	if body.SystemPrompt != nil {
		current.SystemPrompt = *body.SystemPrompt
	}
	if body.Visibility != nil {
		current.Visibility = agent.Visibility(*body.Visibility)
		if err := current.Visibility.Valid(); err != nil {
			Fail(w, errcode.New(errcode.ValidationFailed, "可见性取值不合法"))
			return
		}
	}
	if body.RuntimeType != nil && agent.RuntimeType(*body.RuntimeType) != agent.RuntimeClaudeCode {
		Fail(w, errcode.New(errcode.ValidationFailed, "暂不支持该运行时类型"))
		return
	}
	if body.Model != nil {
		current.Model = *body.Model
	}
	if body.ThinkingLevel != nil {
		current.ThinkingLevel = *body.ThinkingLevel
	}
	if body.CapabilitySet != nil {
		v, err := normalizeJSONObject(body.CapabilitySet)
		if err != nil {
			Fail(w, errcode.New(errcode.ValidationFailed, "capabilitySet 必须是 JSON 对象"))
			return
		}
		current.CapabilitySet = v
	}
	if body.EnvVars != nil {
		v, err := normalizeJSONObject(body.EnvVars)
		if err != nil {
			Fail(w, errcode.New(errcode.ValidationFailed, "envVars 必须是 JSON 对象"))
			return
		}
		current.EnvVars = v
	}
	if body.Enabled != nil {
		current.Enabled = *body.Enabled
	}

	if err := mysqlstore.UpdateAgent(r.Context(), h.DB, current); err != nil {
		if errors.Is(err, mysqlstore.ErrAgentNameTaken) {
			Fail(w, errcode.New(errcode.Conflict, "Agent 名称已存在"))
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			Fail(w, errcode.New(errcode.NotFound, "Agent 不存在"))
			return
		}
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "更新 Agent 失败"))
		return
	}
	Data(w, http.StatusOK, newAgentView(current))
}

// deleteAgent 处理 DELETE /agents/{id}，软删除（enabled=false）。
func (h *ControlPlane) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := mysqlstore.GetAgent(r.Context(), h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, errcode.New(errcode.NotFound, "Agent 不存在"))
		return
	}
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败"))
		return
	}
	actor, ok := h.authorize(w, r, current.WorkspaceID, auth.ActionCreateIssue)
	if !ok {
		return
	}
	if current.OwnerID != actor.UserID &&
		!auth.Can(actor, auth.ActionManageCapabilities, auth.Resource{WorkspaceID: current.WorkspaceID}) {
		Fail(w, errcode.New(errcode.Forbidden, "无权限删除该 Agent"))
		return
	}
	if err := mysqlstore.SetAgentEnabled(r.Context(), h.DB, id, false); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			Fail(w, errcode.New(errcode.NotFound, "Agent 不存在"))
			return
		}
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "停用 Agent 失败"))
		return
	}
	current.Enabled = false
	Data(w, http.StatusOK, newAgentView(current))
}

// machineInWorkspace 校验机器存在且属于指定工作区。
func (h *ControlPlane) machineInWorkspace(w http.ResponseWriter, r *http.Request, machineID, workspaceID string) bool {
	m, err := mysqlstore.GetMachine(r.Context(), h.DB, machineID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && m.WorkspaceID != workspaceID) {
		Fail(w, errcode.New(errcode.ValidationFailed, "机器不存在或不属于该工作区"))
		return false
	}
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取机器失败"))
		return false
	}
	return true
}

// ---------- 仓库 ----------

// listRepositories 处理 GET /repositories?workspaceId=。
func (h *ControlPlane) listRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	if _, ok := h.authorize(w, r, workspaceID, auth.ActionView); !ok {
		return
	}
	repos, err := mysqlstore.ListRepositoriesByWorkspace(r.Context(), h.DB, workspaceID)
	if err != nil {
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取仓库失败"))
		return
	}
	out := make([]repositoryView, 0, len(repos))
	for _, repo := range repos {
		out = append(out, newRepositoryView(repo))
	}
	Data(w, http.StatusOK, out)
}

// createRepository 处理 POST /repositories。
func (h *ControlPlane) createRepository(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceID   string `json:"workspaceId"`
		URL           string `json:"url"`
		DefaultBranch string `json:"defaultBranch"`
		Provider      string `json:"provider"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	actor, ok := h.authorize(w, r, body.WorkspaceID, auth.ActionCreateIssue)
	if !ok {
		return
	}
	url := strings.TrimSpace(body.URL)
	if url == "" || len(url) > 512 || strings.ContainsAny(url, " \t\r\n") {
		Fail(w, errcode.New(errcode.ValidationFailed, "仓库地址不合法"))
		return
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") &&
		!strings.HasPrefix(url, "git@") && !strings.HasPrefix(url, "ssh://") {
		Fail(w, errcode.New(errcode.ValidationFailed, "仓库地址必须是 git 可访问的 URL"))
		return
	}
	defaultBranch := strings.TrimSpace(body.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	provider := body.Provider
	switch provider {
	case "":
		provider = "github"
	case "github", "git":
	default:
		Fail(w, errcode.New(errcode.ValidationFailed, "provider 取值不合法"))
		return
	}

	repo := mysqlstore.Repository{
		ID:            idgen.New(),
		WorkspaceID:   actor.WorkspaceID,
		URL:           url,
		DefaultBranch: defaultBranch,
		Provider:      provider,
	}
	if err := mysqlstore.CreateRepository(r.Context(), h.DB, repo); err != nil {
		if errors.Is(err, mysqlstore.ErrRepositoryTaken) {
			Fail(w, errcode.New(errcode.Conflict, "仓库地址已存在"))
			return
		}
		Fail(w, errcode.New(errcode.UpstreamUnavailable, "创建仓库失败"))
		return
	}
	Data(w, http.StatusCreated, newRepositoryView(repo))
}

// ---------- 小工具 ----------

func validWorkDir(p string) bool {
	p = strings.TrimSpace(p)
	return p != "" && filepath.IsAbs(p)
}

// normalizeJSONObject 校验 JSON 对象；未提供时返回 {}，避免写入 NULL。
func normalizeJSONObject(raw *json.RawMessage) (json.RawMessage, error) {
	if raw == nil {
		return json.RawMessage("{}"), nil
	}
	trimmed := bytes.TrimSpace(*raw)
	if len(trimmed) == 0 {
		return json.RawMessage("{}"), nil
	}
	// 显式 null 视为清空。
	if bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}"), nil
	}
	if trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, errcode.New(errcode.ValidationFailed, "必须是 JSON 对象")
	}
	return json.RawMessage(trimmed), nil
}

// jsonOrEmptyObject 把空值渲染为 {}，保证响应字段稳定。
func jsonOrEmptyObject(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}")
	}
	return trimmed
}

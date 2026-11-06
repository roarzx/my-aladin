//go:build integration

package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// 令牌与用户的对应关系：1 是普通成员，2 是 viewer，4 是另一名成员（非创建者），
// 3 不属于任何工作区。
const (
	cpMemberToken   = "cp-member"
	cpViewerToken   = "cp-viewer"
	cpOtherToken    = "cp-other"
	cpOutsiderToken = "cp-outsider"
)

const cpMemberID uint64 = 1

// tokenAuth 是按明文令牌查表的认证器，只用于集成测试。
type tokenAuth map[string]auth.Actor

func (a tokenAuth) Authenticate(r *http.Request) (auth.Actor, error) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return auth.Actor{}, errcode.New(errcode.Unauthenticated, "缺少令牌")
	}
	actor, ok := a[header[len(prefix):]]
	if !ok {
		return auth.Actor{}, errcode.New(errcode.Unauthenticated, "令牌无效")
	}
	return actor, nil
}

type cpFixture struct {
	db     *sql.DB
	wsID   string
	router http.Handler
}

// newCPFixture 建一个工作区与三类成员，并挂载控制面路由。
func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	db := testkit.NewDBRaw(t)
	f := &cpFixture{db: db, wsID: idgen.New()}

	execSQL(t, db, `INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "控制面")
	execSQL(t, db, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		f.wsID, cpMemberID, string(domain.RoleMember))
	execSQL(t, db, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		f.wsID, 2, string(domain.RoleViewer))
	execSQL(t, db, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		f.wsID, 4, string(domain.RoleMember))

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE tr FROM task_repositories tr JOIN tasks t ON t.id = tr.task_id WHERE t.workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM machine_tokens WHERE machine_id IN (SELECT id FROM machines WHERE workspace_id = ?)`, f.wsID)
		_, _ = db.Exec(`DELETE FROM machines WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM repositories WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspace_members WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})

	members := &mysqlstore.WorkspaceStore{DB: db}
	f.router = httpapi.New(httpapi.Deps{
		Authenticator: tokenAuth{
			cpMemberToken:   {UserID: cpMemberID},
			cpViewerToken:   {UserID: 2},
			cpOtherToken:    {UserID: 4},
			cpOutsiderToken: {UserID: 3},
		},
		Members: members,
		RegisterGlobal: func(r chi.Router) {
			(&httpapi.ControlPlane{DB: db, Members: members}).Mount(r)
			r.Get("/issues/{id}", (&task.Handler{DB: db, Members: members}).Get)
		},
	})
	return f
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("执行 SQL 失败: %v\n%s", err, query)
	}
}

func (f *cpFixture) do(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v，响应 = %s", err, rec.Body.String())
	}
	return body.Data
}

func assertAPIError(t *testing.T, rec *httptest.ResponseRecorder, status int, code errcode.Code) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态 = %d，期望 %d，响应 = %s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code errcode.Code `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一错误体: %s", rec.Body.String())
	}
	if body.Error.Code != code {
		t.Fatalf("错误码 = %s，期望 %s", body.Error.Code, code)
	}
}

// ---------- 响应视图（只声明断言需要的字段） ----------

type apiMachine struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
}

type createdMachine struct {
	Machine   apiMachine `json:"machine"`
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expiresAt"`
}

type issuedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type apiAgent struct {
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
	CapabilitySet json.RawMessage `json:"capabilitySet"`
	Enabled       bool            `json:"enabled"`
}

type apiRepository struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
	Provider      string `json:"provider"`
}

type issueDetail struct {
	Task struct {
		ID string `json:"id"`
	} `json:"task"`
	Agents []struct {
		Ref     string `json:"ref"`
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
		Role    string `json:"role"`
	} `json:"agents"`
	Repositories []struct {
		ID            string `json:"id"`
		URL           string `json:"url"`
		DefaultBranch string `json:"defaultBranch"`
	} `json:"repositories"`
}

// createMachine 通过 API 建机器，返回机器视图与明文令牌。
func (f *cpFixture) createMachine(t *testing.T, name string) createdMachine {
	t.Helper()
	rec := f.do(t, http.MethodPost, "/machines", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","name":"`+name+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建机器状态 = %d，响应 = %s", rec.Code, rec.Body.String())
	}
	return decodeData[createdMachine](t, rec)
}

func TestControlPlaneMachines(t *testing.T) {
	f := newCPFixture(t)

	created := f.createMachine(t, "local-1")
	if created.Token == "" {
		t.Fatal("创建机器未返回明文令牌")
	}
	if created.ExpiresAt.IsZero() {
		t.Fatal("创建机器未返回密钥过期时间")
	}
	if created.Machine.WorkspaceID != f.wsID || created.Machine.Kind != "local" {
		t.Fatalf("机器视图 = %+v", created.Machine)
	}
	if created.Machine.Status != "offline" {
		t.Fatalf("新机器状态 = %q，期望 offline", created.Machine.Status)
	}
	machineID := created.Machine.ID

	// 同名机器重复创建冲突。
	assertAPIError(t, f.do(t, http.MethodPost, "/machines", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","name":"local-1"}`),
		http.StatusConflict, errcode.Conflict)

	// 列表包含刚创建的机器。
	list := decodeData[[]apiMachine](t, f.do(t, http.MethodGet,
		"/machines?workspaceId="+f.wsID, cpMemberToken, ""))
	if len(list) != 1 || list[0].ID != machineID {
		t.Fatalf("机器列表 = %+v", list)
	}

	// 重新签发密钥。
	issued := decodeData[issuedToken](t, f.do(t, http.MethodPost,
		"/machines/"+machineID+"/token", cpMemberToken, ""))
	if issued.Token == "" || issued.Token == created.Token {
		t.Fatalf("重新签发的令牌 = %q", issued.Token)
	}

	// 不存在的机器。
	assertAPIError(t, f.do(t, http.MethodPost, "/machines/"+idgen.New()+"/token", cpMemberToken, ""),
		http.StatusNotFound, errcode.NotFound)

	// viewer 不能创建机器。
	assertAPIError(t, f.do(t, http.MethodPost, "/machines", cpViewerToken,
		`{"workspaceId":"`+f.wsID+`","name":"local-2"}`),
		http.StatusForbidden, errcode.Forbidden)

	// 非成员不能看到机器列表。
	assertAPIError(t, f.do(t, http.MethodGet, "/machines?workspaceId="+f.wsID, cpOutsiderToken, ""),
		http.StatusForbidden, errcode.Forbidden)
}

func TestControlPlaneAgents(t *testing.T) {
	f := newCPFixture(t)
	machineID := f.createMachine(t, "agent-host").Machine.ID

	// 缺省可见性为 private、运行时为 claude_code、启用状态为 true。
	rec := f.do(t, http.MethodPost, "/agents", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","name":"coder","machineId":"`+machineID+`","workDir":"/tmp/work"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建 Agent 状态 = %d，响应 = %s", rec.Code, rec.Body.String())
	}
	created := decodeData[apiAgent](t, rec)
	if created.Visibility != "private" || created.RuntimeType != "claude_code" || !created.Enabled {
		t.Fatalf("Agent 视图 = %+v", created)
	}
	if created.OwnerID != cpMemberID || created.WorkspaceID != f.wsID {
		t.Fatalf("Agent 归属 = %+v", created)
	}
	if string(created.CapabilitySet) != "{}" {
		t.Fatalf("capabilitySet = %s，期望 {}", created.CapabilitySet)
	}

	// 同名 Agent 冲突。
	assertAPIError(t, f.do(t, http.MethodPost, "/agents", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","name":"coder","machineId":"`+machineID+`","workDir":"/tmp/work"}`),
		http.StatusConflict, errcode.Conflict)

	// 机器不属于该工作区。
	assertAPIError(t, f.do(t, http.MethodPost, "/agents", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","name":"stray","machineId":"`+idgen.New()+`","workDir":"/tmp/work"}`),
		http.StatusBadRequest, errcode.ValidationFailed)

	// 另一名成员创建共享 Agent，用于验证可见性过滤。
	shared := decodeData[apiAgent](t, f.do(t, http.MethodPost, "/agents", cpOtherToken,
		`{"workspaceId":"`+f.wsID+`","name":"shared","machineId":"`+machineID+`","workDir":"/tmp/work","visibility":"shared"}`))

	// 非创建者看不到别人私有的 Agent，但能看到共享的。
	list := decodeData[[]apiAgent](t, f.do(t, http.MethodGet,
		"/agents?workspaceId="+f.wsID, cpOtherToken, ""))
	if len(list) != 1 || list[0].ID != shared.ID {
		t.Fatalf("非创建者的 Agent 列表 = %+v", list)
	}

	// 创建者能看到自己的 Agent。
	own := decodeData[[]apiAgent](t, f.do(t, http.MethodGet,
		"/agents?workspaceId="+f.wsID, cpMemberToken, ""))
	if len(own) != 2 {
		t.Fatalf("创建者的 Agent 列表 = %+v", own)
	}

	// PATCH 只覆盖出现的字段。
	patched := decodeData[apiAgent](t, f.do(t, http.MethodPatch, "/agents/"+created.ID, cpMemberToken,
		`{"name":"renamed","model":"claude-sonnet-4","capabilitySet":{"sandbox":true}}`))
	if patched.Name != "renamed" || patched.Model != "claude-sonnet-4" {
		t.Fatalf("PATCH 后 Agent = %+v", patched)
	}
	if string(patched.CapabilitySet) != `{"sandbox":true}` {
		t.Fatalf("PATCH 后 capabilitySet = %s", patched.CapabilitySet)
	}
	if patched.WorkDir != "/tmp/work" {
		t.Fatalf("PATCH 不应改动未提供的 workDir，实际 %q", patched.WorkDir)
	}

	// 跨工作区修改被拒绝。
	assertAPIError(t, f.do(t, http.MethodPatch, "/agents/"+created.ID, cpMemberToken,
		`{"workspaceId":"other-ws","name":"x"}`),
		http.StatusBadRequest, errcode.ValidationFailed)

	// 非创建者不能修改他人的私有 Agent。
	assertAPIError(t, f.do(t, http.MethodPatch, "/agents/"+created.ID, cpOtherToken,
		`{"name":"hijack"}`),
		http.StatusForbidden, errcode.Forbidden)

	// 删除是软删除：enabled=false。
	deleted := decodeData[apiAgent](t, f.do(t, http.MethodDelete, "/agents/"+created.ID, cpMemberToken, ""))
	if deleted.Enabled {
		t.Fatalf("删除后 enabled = true，响应 = %+v", deleted)
	}
}

func TestControlPlaneRepositories(t *testing.T) {
	f := newCPFixture(t)

	rec := f.do(t, http.MethodPost, "/repositories", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","url":"https://example.com/a.git"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建仓库状态 = %d，响应 = %s", rec.Code, rec.Body.String())
	}
	created := decodeData[apiRepository](t, rec)
	if created.DefaultBranch != "main" || created.Provider != "github" {
		t.Fatalf("仓库缺省值 = %+v", created)
	}
	if created.WorkspaceID != f.wsID {
		t.Fatalf("仓库工作区 = %q", created.WorkspaceID)
	}

	explicit := decodeData[apiRepository](t, f.do(t, http.MethodPost, "/repositories", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","url":"git@example.com:b.git","defaultBranch":"develop","provider":"git"}`))
	if explicit.DefaultBranch != "develop" || explicit.Provider != "git" {
		t.Fatalf("仓库显式值 = %+v", explicit)
	}

	// 同工作区 URL 唯一。
	assertAPIError(t, f.do(t, http.MethodPost, "/repositories", cpMemberToken,
		`{"workspaceId":"`+f.wsID+`","url":"https://example.com/a.git"}`),
		http.StatusConflict, errcode.Conflict)

	list := decodeData[[]apiRepository](t, f.do(t, http.MethodGet,
		"/repositories?workspaceId="+f.wsID, cpMemberToken, ""))
	if len(list) != 2 {
		t.Fatalf("仓库列表 = %+v", list)
	}
}

func TestControlPlaneWorkspaceMembership(t *testing.T) {
	f := newCPFixture(t)

	type membership struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	// 测试库可能带有演示数据，这里只断言本用例的工作区出现且角色正确。
	list := decodeData[[]membership](t, f.do(t, http.MethodGet, "/workspaces", cpMemberToken, ""))
	found := false
	for _, item := range list {
		if item.ID == f.wsID {
			found = true
			if item.Role != string(domain.RoleMember) {
				t.Fatalf("工作区角色 = %q，期望 %q", item.Role, domain.RoleMember)
			}
		}
	}
	if !found {
		t.Fatalf("工作区列表未包含 %s: %+v", f.wsID, list)
	}

	// 非成员看不到该工作区。
	for _, item := range decodeData[[]membership](t,
		f.do(t, http.MethodGet, "/workspaces", cpOutsiderToken, "")) {
		if item.ID == f.wsID {
			t.Fatalf("非成员看到了工作区: %+v", item)
		}
	}
}

func TestIssueDetailIncludesAgentsAndRepositories(t *testing.T) {
	f := newCPFixture(t)
	ctx := context.Background()

	machineID := f.createMachine(t, "issue-host").Machine.ID
	planner := agent.Agent{
		ID: idgen.New(), WorkspaceID: f.wsID, Name: "planner", OwnerID: cpMemberID,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: machineID, WorkDir: "/tmp/work",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}
	member := planner
	member.ID = idgen.New()
	member.Name = "member"
	for _, a := range []agent.Agent{planner, member} {
		if err := mysqlstore.CreateAgent(ctx, f.db, a); err != nil {
			t.Fatalf("写入 Agent 失败: %v", err)
		}
	}

	repoID := idgen.New()
	execSQL(t, f.db, `INSERT INTO repositories (id, workspace_id, url, default_branch, provider) VALUES (?, ?, ?, ?, ?)`,
		repoID, f.wsID, "https://example.com/issue.git", "trunk", "git")

	now := time.Now().UTC()
	taskID := idgen.New()
	if err := mysqlstore.InsertTask(ctx, f.db, domain.Task{
		ID: taskID, WorkspaceID: f.wsID, CreatedBy: cpMemberID, Name: "详情",
		Mode: domain.ModeOrdinary, Status: domain.TaskPending,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入任务失败: %v", err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, f.db, taskID, "a1", planner.ID, domain.TaskAgentPlanner); err != nil {
		t.Fatalf("写入任务成员失败: %v", err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, f.db, taskID, "a2", member.ID, domain.TaskAgentMember); err != nil {
		t.Fatalf("写入任务成员失败: %v", err)
	}
	if err := mysqlstore.InsertTaskRepository(ctx, f.db, taskID, repoID, strings.Repeat("a", 40), "trunk"); err != nil {
		t.Fatalf("写入任务仓库失败: %v", err)
	}

	rec := f.do(t, http.MethodGet, "/issues/"+taskID, cpMemberToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("任务详情状态 = %d，响应 = %s", rec.Code, rec.Body.String())
	}
	detail := decodeData[issueDetail](t, rec)
	if detail.Task.ID != taskID {
		t.Fatalf("任务详情 id = %q", detail.Task.ID)
	}
	if len(detail.Agents) != 2 {
		t.Fatalf("任务成员 = %+v", detail.Agents)
	}
	if detail.Agents[0].Ref != "a1" || detail.Agents[0].Role != string(domain.TaskAgentPlanner) ||
		detail.Agents[0].Name != "planner" {
		t.Fatalf("任务成员 = %+v", detail.Agents)
	}
	if len(detail.Repositories) != 1 || detail.Repositories[0].ID != repoID ||
		detail.Repositories[0].URL != "https://example.com/issue.git" ||
		detail.Repositories[0].DefaultBranch != "trunk" {
		t.Fatalf("任务仓库 = %+v", detail.Repositories)
	}

	// 非成员看到 404，不暴露任务是否存在。
	assertAPIError(t, f.do(t, http.MethodGet, "/issues/"+taskID, cpOutsiderToken, ""),
		http.StatusNotFound, errcode.NotFound)
}

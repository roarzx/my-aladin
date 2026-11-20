//go:build integration

package task_test

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
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
)

// cliEnv 是一次测试的工作区/机器/Agent 组合。
type cliEnv struct {
	DB      *sql.DB
	WSID    string
	OtherWS string
	Machine string
	Agent   string
	// AgentOther 是另一个工作区的 Agent。
	AgentOther string
}

// cliFixture 准备两个工作区、两台机器（各有一个 Agent）；ID 每次随机，避免冲突。
func cliFixture(t *testing.T) cliEnv {
	t.Helper()
	db := testkit.NewDBRaw(t)
	ctx := context.Background()
	env := cliEnv{
		DB:         db,
		WSID:       idgen.New(),
		OtherWS:    idgen.New(),
		Machine:    idgen.New(),
		Agent:      idgen.New(),
		AgentOther: idgen.New(),
	}
	// 先登记清理，创建中途失败也不会留下脏数据。
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM task_agents WHERE agent_id IN (?, ?)`, env.Agent, env.AgentOther)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id IN (?, ?)`, env.WSID, env.OtherWS)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id IN (?, ?)`, env.WSID, env.OtherWS)
		_, _ = db.Exec(`DELETE FROM machines WHERE workspace_id IN (?, ?)`, env.WSID, env.OtherWS)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id IN (?, ?)`, env.WSID, env.OtherWS)
	})

	for _, ws := range []string{env.WSID, env.OtherWS} {
		if err := mysqlstore.CreateWorkspace(ctx, db, mysqlstore.Workspace{ID: ws, Name: "cli-" + ws[:8]}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mysqlstore.UpsertMachine(ctx, db, machine.Machine{
		ID: env.Machine, WorkspaceID: env.WSID, Name: "cli-box", Kind: machine.KindLocal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: env.Agent, WorkspaceID: env.WSID, Name: "cli-agent", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode, MachineID: env.Machine,
		WorkDir: "/tmp", Enabled: true, CapabilitySet: json.RawMessage("[]"),
		EnvVars: json.RawMessage("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: env.AgentOther, WorkspaceID: env.OtherWS, Name: "other-agent", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode, MachineID: "other-machine",
		WorkDir: "/tmp", Enabled: true, CapabilitySet: json.RawMessage("[]"),
		EnvVars: json.RawMessage("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	return env
}

func insertIssue(t *testing.T, env cliEnv, workspaceID, name string) string {
	t.Helper()
	agentID := env.Agent
	if workspaceID != env.WSID {
		agentID = env.AgentOther
	}
	created, err := (&task.Service{DB: env.DB}).Create(context.Background(), 1, workspaceID, task.CreateRequest{
		Name: name, Mode: domain.ModeOrdinary, AgentIDs: []string{agentID}, PlannerAgentID: agentID,
	})
	if err != nil {
		t.Fatalf("创建 Issue: %v", err)
	}
	return created.ID
}

// cliServer 把 CLIHandler 挂在 /api/v1/cli 下，并用机器身份注入中间件。
func cliServer(db *sql.DB, machineID string) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/cli", func(r chi.Router) {
		r.Use(httpapi.MachineAuth(func(*http.Request) (string, error) { return machineID, nil }))
		handler := &task.CLIHandler{DB: db, Service: &task.Service{DB: db}}
		r.Get("/issues/{id}", handler.Get)
		r.Post("/issues/{id}/status", handler.UpdateStatus)
		r.Post("/issues/{id}/input-requests", handler.InputRequest)
	})
	return r
}

func doCLI(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var envelope map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	return rec.Code, envelope
}

func TestCLIUpdateStatusDrivesTaskState(t *testing.T) {
	env := cliFixture(t)
	issueID := insertIssue(t, env, env.WSID, "cli 上报")
	h := cliServer(env.DB, env.Machine)

	code, envelope := doCLI(t, h, http.MethodPost, "/api/v1/cli/issues/"+issueID+"/status",
		`{"status":"in_progress","reason":"开工"}`)
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%v", code, envelope)
	}
	data, _ := envelope["data"].(map[string]any)
	if data["status"] != "in_progress" {
		t.Fatalf("返回状态 = %v", data["status"])
	}

	loaded, err := mysqlstore.GetTask(context.Background(), env.DB, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domain.TaskExecuting {
		t.Fatalf("库内状态 = %q", loaded.Status)
	}

	// 完成后再回进行中（用户追问），仍然允许。
	if code, _ := doCLI(t, h, http.MethodPost, "/api/v1/cli/issues/"+issueID+"/status",
		`{"status":"completed","reason":"跑通测试"}`); code != http.StatusOK {
		t.Fatalf("上报完成失败: %d", code)
	}
	if code, _ := doCLI(t, h, http.MethodPost, "/api/v1/cli/issues/"+issueID+"/status",
		`{"status":"in_progress"}`); code != http.StatusOK {
		t.Fatalf("完成后应可回到进行中: %d", code)
	}

	// 非法状态被拒绝。
	if code, _ := doCLI(t, h, http.MethodPost, "/api/v1/cli/issues/"+issueID+"/status",
		`{"status":"flying"}`); code != http.StatusBadRequest {
		t.Fatalf("非法状态应 400，实际 %d", code)
	}
}

func TestCLIIsolationAndMachineBinding(t *testing.T) {
	env := cliFixture(t)
	issueID := insertIssue(t, env, env.WSID, "cli 隔离")
	h := cliServer(env.DB, env.Machine)

	// 别的工作区的 Issue 对本机器不可见。
	otherID := insertIssue(t, env, env.OtherWS, "别的空间")
	if code, _ := doCLI(t, h, http.MethodGet, "/api/v1/cli/issues/"+otherID, ""); code != http.StatusNotFound {
		t.Fatalf("跨工作区读取应 404，实际 %d", code)
	}
	if code, _ := doCLI(t, h, http.MethodPost,
		"/api/v1/cli/issues/"+otherID+"/status", `{"status":"completed"}`); code != http.StatusNotFound {
		t.Fatalf("跨工作区写入应 404，实际 %d", code)
	}

	// 同工作区但没有本机 Agent 的 Issue 不能上报。
	orphan := insertOrphanIssue(t, env)
	if code, _ := doCLI(t, h, http.MethodPost,
		"/api/v1/cli/issues/"+orphan+"/status", `{"status":"completed"}`); code != http.StatusForbidden {
		t.Fatalf("未绑定本机 Agent 应 403，实际 %d", code)
	}

	// 正常写入。
	if code, _ := doCLI(t, h, http.MethodPost,
		"/api/v1/cli/issues/"+issueID+"/status", `{"status":"completed"}`); code != http.StatusOK {
		t.Fatalf("绑定机器应可上报，实际 %d", code)
	}
}

func TestCLIInputRequestRecordsActivity(t *testing.T) {
	env := cliFixture(t)
	issueID := insertIssue(t, env, env.WSID, "cli 提问")
	h := cliServer(env.DB, env.Machine)

	code, envelope := doCLI(t, h, http.MethodPost, "/api/v1/cli/issues/"+issueID+"/input-requests",
		`{"reason":"plugin_capability_clarification","message":"用哪个分支？","options":["main","dev"]}`)
	if code != http.StatusCreated {
		t.Fatalf("code = %d body=%v", code, envelope)
	}

	var kind string
	if err := env.DB.QueryRow(`SELECT kind FROM activities WHERE task_id = ? ORDER BY seq DESC LIMIT 1`,
		issueID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "issue.input_requested" {
		t.Fatalf("活动类型 = %q", kind)
	}

	// 状态不受影响：Agent 自己用 update-status 决定。
	loaded, err := mysqlstore.GetTask(context.Background(), env.DB, issueID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domain.TaskPending {
		t.Fatalf("请求输入不应改状态，实际 %q", loaded.Status)
	}
}

// insertOrphanIssue 建一个没有绑定本机 Agent 的 Issue。
func insertOrphanIssue(t *testing.T, env cliEnv) string {
	t.Helper()
	ctx := context.Background()
	taskID := idgen.New()
	raw, _ := json.Marshal(map[string]any{"mode": "ordinary"})
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, env.DB, domain.Task{
		ID: taskID, WorkspaceID: env.WSID, CreatedBy: 1, Name: "孤儿 Issue",
		Mode: domain.ModeOrdinary, Status: domain.TaskPending, ConfigSnapshot: raw,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("插入孤儿 Issue: %v", err)
	}
	return taskID
}

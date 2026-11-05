//go:build integration

package task_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fixture struct {
	db        *sql.DB
	wsID      string
	machineID string
	planner   agent.Agent
	member    agent.Agent
	repoID    string
	tplID     string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)

	f := &fixture{
		db:        db,
		wsID:      idgen.New(),
		machineID: idgen.New(),
		repoID:    idgen.New(),
		tplID:     idgen.New(),
	}

	mustExec(t, db, `INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "tasks")

	mkAgent := func(name string, owner uint64, vis agent.Visibility) agent.Agent {
		return agent.Agent{
			ID: idgen.New(), WorkspaceID: f.wsID, Name: name, OwnerID: owner,
			Visibility: vis, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: f.machineID, WorkDir: "/tmp/work", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`),
			EnvVars:       json.RawMessage(`{}`),
			Enabled:       true,
		}
	}
	f.planner = mkAgent("planner", 1, agent.Shared)
	f.member = mkAgent("member", 1, agent.Shared)
	for _, a := range []agent.Agent{f.planner, f.member} {
		if err := mysqlstore.CreateAgent(ctx, db, a); err != nil {
			t.Fatalf("创建 Agent: %v", err)
		}
	}

	mustExec(t, db, `INSERT INTO repositories (id, workspace_id, url) VALUES (?, ?, ?)`,
		f.repoID, f.wsID, "https://example.com/repo.git")
	mustExec(t, db, `INSERT INTO task_templates (id, workspace_id, name, config) VALUES (?, ?, ?, ?)`,
		f.tplID, f.wsID, "default", `{"constraints":["no force push"]}`)

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM task_agents WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, f.wsID)
		_, _ = db.Exec(`DELETE FROM task_repositories WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, f.wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM task_templates WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM repositories WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("执行 SQL 失败: %v", err)
	}
}

func (f *fixture) request() task.CreateRequest {
	return task.CreateRequest{
		Name:           "实现新接口",
		Mode:           domain.ModeOrdinary,
		TemplateID:     f.tplID,
		AgentIDs:       []string{f.planner.ID, f.member.ID},
		PlannerAgentID: f.planner.ID,
		Repositories:   []string{f.repoID},
	}
}

func TestCreateTask(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := &task.Service{DB: f.db}

	tk, err := svc.Create(ctx, 1, f.wsID, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tk.Status != domain.TaskPending || tk.CreatedBy != 1 || tk.WorkspaceID != f.wsID {
		t.Fatalf("任务字段 = %+v", tk)
	}

	stored, err := mysqlstore.GetTask(ctx, f.db, tk.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	rows, err := f.db.Query(
		`SELECT agent_ref, agent_id, role FROM task_agents WHERE task_id = ? ORDER BY agent_ref`, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type ta struct{ ref, agentID, role string }
	var got []ta
	for rows.Next() {
		var r ta
		if err := rows.Scan(&r.ref, &r.agentID, &r.role); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := []ta{
		{"a1", f.planner.ID, "planner"},
		{"a2", f.member.ID, "member"},
	}
	if len(got) != len(want) {
		t.Fatalf("task_agents = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task_agents[%d] = %+v, 期望 %+v", i, got[i], want[i])
		}
	}

	var repoCount int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM task_repositories WHERE task_id = ?`, tk.ID).Scan(&repoCount); err != nil {
		t.Fatal(err)
	}
	if repoCount != 1 {
		t.Fatalf("task_repositories = %d", repoCount)
	}

	var snap struct {
		Template *struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"template"`
		TemplateConfig struct {
			Constraints []string `json:"constraints"`
		} `json:"templateConfig"`
		Mode           string `json:"mode"`
		PlannerAgentID string `json:"plannerAgentId"`
		Agents         []struct {
			Ref     string `json:"ref"`
			AgentID string `json:"agentId"`
			Role    string `json:"role"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(stored.ConfigSnapshot, &snap); err != nil {
		t.Fatalf("解析快照: %v\n%s", err, stored.ConfigSnapshot)
	}
	if snap.Template == nil || snap.Template.ID != f.tplID || snap.Template.Version != 1 {
		t.Fatalf("快照模板 = %+v", snap.Template)
	}
	if len(snap.TemplateConfig.Constraints) != 1 || snap.TemplateConfig.Constraints[0] != "no force push" {
		t.Fatalf("模板配置未合并: %+v", snap.TemplateConfig)
	}
	if snap.PlannerAgentID != f.planner.ID || snap.Mode != "ordinary" || len(snap.Agents) != 2 {
		t.Fatalf("快照内容 = %+v", snap)
	}
}

func TestCreateTaskRejectsUnschedulableAgent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	private := agent.Agent{
		ID: idgen.New(), WorkspaceID: f.wsID, Name: "private-other", OwnerID: 2,
		Visibility: agent.Private, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: f.machineID, WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}
	if err := mysqlstore.CreateAgent(ctx, f.db, private); err != nil {
		t.Fatal(err)
	}

	req := f.request()
	req.AgentIDs = []string{f.planner.ID, private.ID}
	req.PlannerAgentID = f.planner.ID

	_, err := (&task.Service{DB: f.db}).Create(ctx, 1, f.wsID, req)
	assertCode(t, err, errcode.AgentUnavailable)
}

func TestCreateTaskTemplateNotFound(t *testing.T) {
	f := setup(t)
	req := f.request()
	req.TemplateID = idgen.New()

	_, err := (&task.Service{DB: f.db}).Create(context.Background(), 1, f.wsID, req)
	assertCode(t, err, errcode.TemplateNotFound)
}

func TestCreateTaskCrossWorkspaceAgent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	otherWS := idgen.New()
	mustExec(t, f.db, `INSERT INTO workspaces (id, name) VALUES (?, ?)`, otherWS, "other")
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM workspaces WHERE id = ?`, otherWS) })

	foreign := agent.Agent{
		ID: idgen.New(), WorkspaceID: otherWS, Name: "foreign", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: f.machineID, WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}
	if err := mysqlstore.CreateAgent(ctx, f.db, foreign); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM agents WHERE id = ?`, foreign.ID) })

	req := f.request()
	req.AgentIDs = []string{f.planner.ID, foreign.ID}

	_, err := (&task.Service{DB: f.db}).Create(ctx, 1, f.wsID, req)
	assertCode(t, err, errcode.AgentUnavailable)
}

func assertCode(t *testing.T, err error, want errcode.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望失败 %s，实际成功", want)
	}
	e, ok := errcode.As(err)
	if !ok || e.Code != want {
		t.Fatalf("错误码 = %v，期望 %s", err, want)
	}
}

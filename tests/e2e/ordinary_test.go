//go:build e2e

// Package e2e 是端到端验收：真实 MySQL/Redis + 可控 Runtime。
package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/issue"
	"github.com/roarzx/my-aladin/internal/machine"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

type recordingRuntime struct {
	mu       sync.Mutex
	requests []runtime.DispatchRequest
	cancels  []string
	failWith error
}

func (r *recordingRuntime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	if r.failWith != nil {
		return "", r.failWith
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return req.ExecutionID, nil
}

func (r *recordingRuntime) Cancel(_ context.Context, executionID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels = append(r.cancels, executionID)
	return true, nil
}

func (r *recordingRuntime) Capabilities() runtime.CapabilitySet { return runtime.CapabilitySet{} }

func (r *recordingRuntime) last() runtime.DispatchRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1]
}

func (r *recordingRuntime) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

type e2eEnv struct {
	db      *sql.DB
	wsID    string
	machine string
	agentID string
	runtime *recordingRuntime
	service *issue.Service
	events  *issue.EventHandler
}

func setupIssue(t *testing.T) *e2eEnv {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	env := &e2eEnv{
		db: db, wsID: idgen.New(), machine: idgen.New(), agentID: idgen.New(),
		runtime: &recordingRuntime{},
	}
	env.service = &issue.Service{DB: db, Runtime: env.runtime}
	env.events = &issue.EventHandler{Service: env.service}

	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, env.wsID, "e2e"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, 1, 'owner')`, env.wsID); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.UpsertMachine(ctx, db, machine.Machine{
		ID: env.machine, WorkspaceID: env.wsID, Name: "e2e-box", Kind: machine.KindLocal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: env.agentID, WorkspaceID: env.wsID, Name: "e2e-agent", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: env.machine, WorkDir: "/tmp/e2e", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM issue_runs WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM machines WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM workspace_members WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, env.wsID)
	})
	return env
}

func (e *e2eEnv) createIssue(t *testing.T, requirement string) string {
	t.Helper()
	created, err := (&task.Service{DB: e.db}).Create(context.Background(), 1, e.wsID, task.CreateRequest{
		Name: "e2e", Requirement: requirement, Mode: domain.ModeOrdinary,
		AgentIDs: []string{e.agentID}, PlannerAgentID: e.agentID,
	})
	if err != nil {
		t.Fatalf("创建 Issue: %v", err)
	}
	return created.ID
}

func (e *e2eEnv) event(t *testing.T, kind, executionID string, data map[string]any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(protocol.EventPayload{Kind: kind, Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	env := protocol.Envelope{Type: protocol.TypeEvent, ExecutionID: executionID, Payload: payload}
	if err := e.events.Handle(context.Background(), "machine-e2e", env); err != nil {
		t.Fatalf("处理事件 %s: %v", kind, err)
	}
}

func (e *e2eEnv) status(t *testing.T, taskID string) domain.TaskStatus {
	t.Helper()
	row, err := mysqlstore.GetTask(context.Background(), e.db, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return row.Status
}

func (e *e2eEnv) activityKinds(t *testing.T, taskID string) []string {
	t.Helper()
	items, err := mysqlstore.ListActivities(context.Background(), e.db, taskID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		kinds = append(kinds, item.Kind)
	}
	return kinds
}

func TestIssueTurnLifecycle(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "做一个登录页")

	// 1. 开始一轮：派发并置为执行中。
	run, err := env.service.StartTurn(ctx, taskID, "做一个登录页")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if env.runtime.count() != 1 {
		t.Fatalf("派发次数 = %d", env.runtime.count())
	}
	first := env.runtime.last()
	if first.IssueID != taskID || first.Prompt != "做一个登录页" || first.ACPSessionID != "" {
		t.Fatalf("派发参数 = %+v", first)
	}
	if env.status(t, taskID) != domain.TaskExecuting {
		t.Fatalf("派发后状态 = %q", env.status(t, taskID))
	}

	// 2. 重复开始被拒（同一时间只有一轮）。
	if _, err := env.service.StartTurn(ctx, taskID, "再来一次"); err == nil {
		t.Fatal("执行中不应允许再开一轮")
	}

	// 3. 回合结束：回复入会话流，但状态仍由 Agent 决定。
	env.event(t, "turn_done", run.ExecutionID, map[string]any{
		"reply": "已写好登录页", "acp_session_id": "acp-1", "duration_ms": 1200,
	})
	if env.status(t, taskID) != domain.TaskExecuting {
		t.Fatalf("turn_done 不应改状态，实际 = %q", env.status(t, taskID))
	}
	if kinds := env.activityKinds(t, taskID); !contains(kinds, "message.assistant") || !contains(kinds, "turn.done") {
		t.Fatalf("活动流 = %v", kinds)
	}
	session, err := mysqlstore.FindSessionByKind(ctx, env.db, taskID, env.agentID, "issue")
	if err != nil || session.ACPSessionID != "acp-1" {
		t.Fatalf("会话 = %+v, err = %v", session, err)
	}

	// 4. 用户追问：复用同一会话与 acp_session_id。
	follow, err := env.service.Message(ctx, taskID, "按钮再大一点")
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	second := env.runtime.last()
	if second.ACPSessionID != "acp-1" {
		t.Fatalf("追问应复用会话，实际 acp_session_id = %q", second.ACPSessionID)
	}
	if second.SessionID != session.ID {
		t.Fatalf("追问应复用会话行: %s != %s", second.SessionID, session.ID)
	}
	_ = follow

	// 5. 取消：停止执行并置为已取消。
	if err := env.service.Cancel(ctx, taskID, "不做了"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(env.runtime.cancels) != 1 {
		t.Fatalf("取消派发 = %v", env.runtime.cancels)
	}
	if env.status(t, taskID) != domain.TaskCancelled {
		t.Fatalf("取消后状态 = %q", env.status(t, taskID))
	}

	// 6. 迟到的 turn_done 不得复活或改写状态。
	env.event(t, "turn_done", second.ExecutionID, map[string]any{"reply": "迟到的回复"})
	if env.status(t, taskID) != domain.TaskCancelled {
		t.Fatalf("迟到回执后状态 = %q", env.status(t, taskID))
	}
}

func TestIssueTurnFailKeepsStatus(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "跑测试")

	run, err := env.service.StartTurn(ctx, taskID, "跑测试")
	if err != nil {
		t.Fatal(err)
	}
	env.event(t, "turn_fail", run.ExecutionID, map[string]any{
		"reason": "agent 崩了", "error_class": "runtime",
	})
	if env.status(t, taskID) != domain.TaskExecuting {
		t.Fatalf("失败不应改状态，实际 = %q", env.status(t, taskID))
	}
	if kinds := env.activityKinds(t, taskID); !contains(kinds, "turn.failed") {
		t.Fatalf("活动流 = %v", kinds)
	}
	// 下一轮可以继续（上一轮已结束）。
	if _, err := env.service.StartTurn(ctx, taskID, "再试一次"); err != nil {
		t.Fatalf("失败后应可重试: %v", err)
	}
}

func TestIssueTurnDispatchFailureRollsBack(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "派发失败")
	env.runtime.failWith = context.DeadlineExceeded

	if _, err := env.service.StartTurn(ctx, taskID, "x"); err == nil {
		t.Fatal("派发失败应返回错误")
	}
	if env.status(t, taskID) != domain.TaskPending {
		t.Fatalf("派发失败应回滚状态，实际 = %q", env.status(t, taskID))
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestFailStaleRunsUnblocksIssue(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "daemon 重启后的收尾")

	run, err := env.service.StartTurn(ctx, taskID, "干活")
	if err != nil {
		t.Fatal(err)
	}
	// 模拟 daemon 进程消失：执行记录停在进行中，没人会来收尾。
	if _, err := env.db.Exec(`UPDATE issue_runs SET status = 'running' WHERE id = ?`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.StartTurn(ctx, taskID, "再开一轮"); err == nil {
		t.Fatal("有未收尾执行时应拒绝新的一轮")
	}

	if failed := env.service.FailStaleRuns(ctx, env.machine); failed != 1 {
		t.Fatalf("应清理 1 条未收尾执行，实际 %d", failed)
	}
	if _, err := env.service.StartTurn(ctx, taskID, "再开一轮"); err != nil {
		t.Fatalf("清理后应能继续: %v", err)
	}
}

func TestEvalModePausesAfterTurn(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "评测任务")
	if _, err := env.db.Exec(`UPDATE agents SET eval_mode = 1 WHERE id = ?`, env.agentID); err != nil {
		t.Fatal(err)
	}

	run, err := env.service.StartTurn(ctx, taskID, "修一下")
	if err != nil {
		t.Fatal(err)
	}
	env.event(t, "turn_done", run.ExecutionID, map[string]any{"reply": "改好了"})

	if got := env.status(t, taskID); got != domain.TaskPaused {
		t.Fatalf("评测轮结束后应为 paused（等待验收），实际 %q", got)
	}
}

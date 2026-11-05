//go:build e2e

// Package e2e 是端到端验收（I1/I2）：真实 MySQL/Redis + 可控 Runtime。
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
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/plan"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

type recordingRuntime struct {
	mu       sync.Mutex
	requests []runtime.DispatchRequest
}

func (r *recordingRuntime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return req.ExecutionID, nil
}

func (r *recordingRuntime) Inspect(context.Context, string) (runtime.ExecutionSnapshot, error) {
	return runtime.ExecutionSnapshot{State: runtime.StateUnknown}, nil
}

func (r *recordingRuntime) Cancel(context.Context, string) (bool, error) { return true, nil }

func (r *recordingRuntime) Capabilities() runtime.CapabilitySet { return runtime.CapabilitySet{} }

func (r *recordingRuntime) last() runtime.DispatchRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1]
}

type e2eEnv struct {
	db           *sql.DB
	wsID         string
	agentID      string
	secondAgent  string
	runtime      *recordingRuntime
	runner       *orchestration.PlanRunner
	handler      *orchestration.EventHandler
	canceller    *orchestration.Canceller
	retryService *orchestration.RetryService
}

func setupOrdinary(t *testing.T) *e2eEnv {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	env := &e2eEnv{
		db: db, wsID: idgen.New(), agentID: idgen.New(), secondAgent: idgen.New(),
		runtime: &recordingRuntime{},
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, env.wsID, "e2e"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, 1, 'owner')`, env.wsID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{env.agentID, env.secondAgent} {
		if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
			ID: id, WorkspaceID: env.wsID, Name: id[:8], OwnerID: 1,
			Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: "machine-e2e", WorkDir: "/tmp/e2e", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	env.runner = &orchestration.PlanRunner{DB: db, Runtime: env.runtime, Owner: "e2e"}
	env.handler = &orchestration.EventHandler{DB: db, Runner: env.runner}
	env.canceller = &orchestration.Canceller{DB: db, Runtime: env.runtime}
	env.retryService = &orchestration.RetryService{DB: db, Runtime: env.runtime, MaxAttempts: 5}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id IN (SELECT id FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)))`, env.wsID)
		_, _ = db.Exec(`DELETE FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?))`, env.wsID)
		_, _ = db.Exec(`DELETE FROM confirmations WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?))`, env.wsID)
		_, _ = db.Exec(`DELETE FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, env.wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM workspace_members WHERE workspace_id = ?`, env.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, env.wsID)
	})
	return env
}

func (e *e2eEnv) createTask(t *testing.T, steps []plan.StepInput) (string, string) {
	t.Helper()
	ctx := context.Background()
	svc := &task.Service{DB: e.db}
	created, err := svc.Create(ctx, 1, e.wsID, task.CreateRequest{
		Name: "e2e", Mode: domain.ModeOrdinary,
		AgentIDs: []string{e.agentID, e.secondAgent}, PlannerAgentID: e.agentID,
	})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	planSvc := &plan.Service{DB: e.db}
	proposal, _, err := planSvc.Propose(ctx, created.ID, plan.Proposal{
		ContentMD: "# plan", Revision: 1, Steps: steps,
	})
	if err != nil {
		t.Fatalf("提交计划: %v", err)
	}
	if _, err := planSvc.Confirm(ctx, proposal.ID, 1, proposal.ContentHash, 1); err != nil {
		t.Fatalf("确认计划: %v", err)
	}
	current, _ := mysqlstore.GetTask(ctx, e.db, created.ID)
	if _, err := planSvc.Start(ctx, created.ID, current.Version); err != nil {
		t.Fatalf("启动任务: %v", err)
	}
	return created.ID, proposal.ID
}

func (e *e2eEnv) event(t *testing.T, kind, executionID string, attemptNo uint32, data map[string]any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(protocol.EventPayload{Kind: kind, Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	attempt := attemptNo
	env := protocol.Envelope{
		Type: protocol.TypeEvent, ExecutionID: executionID,
		ExecutionAttempt: &attempt, Payload: payload,
	}
	if err := e.handler.Handle(context.Background(), "machine-e2e", env); err != nil {
		t.Fatalf("处理事件 %s: %v", kind, err)
	}
}

func TestOrdinary(t *testing.T) {
	env := setupOrdinary(t)
	ctx := context.Background()
	taskID, planID := env.createTask(t, []plan.StepInput{
		{Ordinal: 1, Title: "第一步", Body: "b", AgentRef: "a1"},
		{Ordinal: 2, Title: "第二步", Body: "b", AgentRef: "a2"},
	})

	// 第一步派发并完成
	if err := env.runner.RunOnce(ctx, taskID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	first := env.runtime.last()
	env.event(t, "step_done", first.ExecutionID, first.AttemptNo,
		map[string]any{"attempt_no": first.AttemptNo, "commit_sha": "abc"})

	// 第二步派发后失败（verification → 暂停）
	second := env.runtime.last()
	env.event(t, "step_fail", second.ExecutionID, second.AttemptNo,
		map[string]any{"attempt_no": second.AttemptNo, "reason": "测试失败",
			"error_class": "verification", "uncertain": false})
	taskRow, _ := mysqlstore.GetTask(ctx, env.db, taskID)
	if taskRow.Status != domain.TaskPaused {
		t.Fatalf("失败后 task = %q", taskRow.Status)
	}
	_, steps, _ := mysqlstore.GetPlanByID(ctx, env.db, planID)
	if steps[1].Status != domain.StepFailed {
		t.Fatalf("第二步 = %q", steps[1].Status)
	}

	// 人工重试第二步
	attemptNo, err := env.retryService.Retry(ctx, steps[1].ID, "再试一次")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	retryDispatch := env.runtime.last()
	if retryDispatch.AttemptNo != attemptNo {
		t.Fatalf("重试 attempt = %d, dispatch = %d", attemptNo, retryDispatch.AttemptNo)
	}
	env.event(t, "step_done", retryDispatch.ExecutionID, retryDispatch.AttemptNo,
		map[string]any{"attempt_no": retryDispatch.AttemptNo})

	taskRow, _ = mysqlstore.GetTask(ctx, env.db, taskID)
	if taskRow.Status != domain.TaskChatting {
		t.Fatalf("完成后 task = %q", taskRow.Status)
	}
	activities, _ := mysqlstore.ListActivities(ctx, env.db, taskID, 0, 50)
	kinds := make([]string, 0, len(activities))
	for _, activity := range activities {
		kinds = append(kinds, activity.Kind)
	}
	t.Logf("events = %v", kinds)
}

func TestOrdinaryCancelRecovery(t *testing.T) {
	env := setupOrdinary(t)
	ctx := context.Background()
	taskID, _ := env.createTask(t, []plan.StepInput{
		{Ordinal: 1, Title: "唯一一步", Body: "b", AgentRef: "a1"},
	})
	if err := env.runner.RunOnce(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	dispatch := env.runtime.last()

	if _, allAcked, err := env.canceller.RequestCancel(ctx, taskID); err != nil || !allAcked {
		t.Fatalf("RequestCancel: %v, acked=%v", err, allAcked)
	}
	if err := env.canceller.FinalizeCancel(ctx, taskID, false); err != nil {
		t.Fatalf("FinalizeCancel: %v", err)
	}

	// 迟到的成功回调不得复活任务
	env.event(t, "step_done", dispatch.ExecutionID, dispatch.AttemptNo,
		map[string]any{"attempt_no": dispatch.AttemptNo})
	taskRow, _ := mysqlstore.GetTask(ctx, env.db, taskID)
	if taskRow.Status != domain.TaskCancelled {
		t.Fatalf("取消后 task = %q", taskRow.Status)
	}
	_, steps, _ := mysqlstore.GetPlanByID(ctx, env.db, mustPlanID(t, env.db, taskID))
	if steps[0].Status != domain.StepCancelled {
		t.Fatalf("取消后 step = %q", steps[0].Status)
	}
}

func mustPlanID(t *testing.T, db *sql.DB, taskID string) string {
	t.Helper()
	revision, err := mysqlstore.LatestPlanRevision(context.Background(), db, taskID)
	if err != nil || revision == 0 {
		t.Fatalf("读取计划版本: %v", err)
	}
	planRow, _, err := mysqlstore.GetPlanByRevision(context.Background(), db, taskID, revision)
	if err != nil {
		t.Fatal(err)
	}
	return planRow.ID
}

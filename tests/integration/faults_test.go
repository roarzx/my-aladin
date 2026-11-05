//go:build integration

// Package integration_test 是普通模式的故障注入套件（E10，dev_spec 4.2 场景 1–6、8）。
//
// 未在此文件覆盖的场景及其归属：
//
//	7  → F 阶段（小队冻结与审批）
//	9  → B 阶段（跨 Workspace 隔离）
//	10 → C 阶段（跨机器 HEAD / 脏工作区）
//	11、12 → G 阶段（FlowRun / PR 对账）
package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/agentctx"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/plan"
	"github.com/roarzx/my-aladin/internal/recovery"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

type faultFixture struct {
	db         *sql.DB
	wsID       string
	taskID     string
	planID     string
	stepID     string
	agentID    string
	taskStatus domain.TaskStatus
	stepStatus domain.StepStatus
}

func newFaultFixture(t *testing.T, taskStatus domain.TaskStatus, stepStatus domain.StepStatus) *faultFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &faultFixture{
		db: db, wsID: idgen.New(), taskID: idgen.New(), planID: idgen.New(),
		stepID: idgen.New(), agentID: idgen.New(),
		taskStatus: taskStatus, stepStatus: stepStatus,
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "faults"); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: f.agentID, WorkspaceID: f.wsID, Name: "a", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: "machine-fault", WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "fault",
		Mode: domain.ModeOrdinary, Status: taskStatus,
		PlanBranch: "aladdin/fault", ConfigSnapshot: json.RawMessage(`{}`),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, f.taskID, "a1", f.agentID, domain.TaskAgentPlanner); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertPlan(ctx, db, domain.Plan{
		ID: f.planID, TaskID: f.taskID, Revision: 1, ContentHash: "h",
		ContentMD: "# plan", Status: domain.PlanExecuting,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertPlanStep(ctx, db, domain.Step{
		ID: f.stepID, PlanID: f.planID, Ordinal: 1, Title: "s", Body: "b",
		AgentRef: "a1", Status: stepStatus,
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id = ?`, f.stepID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM context_entries WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func (f *faultFixture) insertAttempt(t *testing.T, status domain.AttemptStatus) domain.Attempt {
	t.Helper()
	attempt := domain.Attempt{
		ID: idgen.New(), StepID: f.stepID,
		ExecutionID: idgen.New(), LeaseEpoch: 1, Status: status,
	}
	// attempt_no 必须唯一：按已有数量递增
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM attempts WHERE step_id = ?`, f.stepID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	attempt.AttemptNo = uint32(count + 1)
	if err := mysqlstore.InsertAttempt(context.Background(), f.db, attempt); err != nil {
		t.Fatal(err)
	}
	return attempt
}

type faultRuntime struct {
	mu       sync.Mutex
	requests []runtime.DispatchRequest
}

func (r *faultRuntime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return req.ExecutionID, nil
}

func (r *faultRuntime) Inspect(context.Context, string) (runtime.ExecutionSnapshot, error) {
	return runtime.ExecutionSnapshot{State: runtime.StateUnknown}, nil
}

func (r *faultRuntime) Cancel(context.Context, string) (bool, error) { return true, nil }

func (r *faultRuntime) Capabilities() runtime.CapabilitySet { return runtime.CapabilitySet{} }

func (r *faultRuntime) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// 场景 1：确认后修改 Plan，再启动应拒绝；重复确认不增加审批效果。
func TestFaultPlanModifiedAfterConfirm(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	wsID, taskID := idgen.New(), idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "fault-plan"); err != nil {
		t.Fatal(err)
	}
	planner := agent.Agent{
		ID: idgen.New(), WorkspaceID: wsID, Name: "p", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: "m", WorkDir: "/tmp", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}
	if err := mysqlstore.CreateAgent(ctx, db, planner); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: taskID, WorkspaceID: wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskPending,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, taskID, "a1", planner.ID, domain.TaskAgentPlanner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM confirmations WHERE plan_id IN (SELECT id FROM plans WHERE task_id = ?)`, taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})

	svc := &plan.Service{DB: db}
	proposal := func(rev int) plan.Proposal {
		return plan.Proposal{
			ContentMD: "# plan", Revision: rev,
			Steps: []plan.StepInput{{Ordinal: 1, Title: "s", Body: "b", AgentRef: "a1"}},
		}
	}
	p1, _, err := svc.Propose(ctx, taskID, proposal(1))
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := svc.Confirm(ctx, p1.ID, 1, p1.ContentHash, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 重复确认幂等，不增加审批效果
	again, err := svc.Confirm(ctx, p1.ID, 1, p1.ContentHash, 1)
	if err != nil || again.ID != confirmation.ID {
		t.Fatalf("重复确认不幂等: %+v, %v", again, err)
	}

	// 确认后修改计划 → 旧确认失效，启动被拒
	if _, _, err := svc.Propose(ctx, taskID, proposal(2)); err != nil {
		t.Fatal(err)
	}
	task, _ := mysqlstore.GetTask(ctx, db, taskID)
	if _, err := svc.Start(ctx, taskID, task.Version); !hasCode(err, errcode.NotConfirmed) {
		t.Fatalf("期望 NOT_CONFIRMED，实际: %v", err)
	}
}

// 场景 2：两个 Runner 同时抢占同一步骤，只产生一个有效执行资格。
func TestFaultTwoRunnersOneExecution(t *testing.T) {
	f := newFaultFixture(t, domain.TaskExecuting, domain.StepPending)
	ctx := context.Background()
	client := testkit.NewRedis(t)
	runtimeStub := &faultRuntime{}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		runner := &orchestration.PlanRunner{
			DB: f.db, Runtime: runtimeStub, Owner: "runner",
			Lock: orchestration.NewTaskLock(client, time.Second, -1),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = runner.RunOnce(ctx, f.taskID)
		}()
	}
	wg.Wait()

	if runtimeStub.count() != 1 {
		t.Fatalf("派发次数 = %d，期望 1", runtimeStub.count())
	}
	var attempts int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM attempts WHERE step_id = ?`, f.stepID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d，期望 1", attempts)
	}
}

// 场景 3：状态已提交、WS 消息未送出时崩溃，客户端凭游标补拉不丢事件。
func TestFaultCursorBackfill(t *testing.T) {
	f := newFaultFixture(t, domain.TaskExecuting, domain.StepPending)
	ctx := context.Background()

	first, err := mysqlstore.AppendActivity(ctx, f.db, f.taskID, "task.created", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.AppendActivity(ctx, f.db, f.taskID, "step.dispatched", nil); err != nil {
		t.Fatal(err)
	}
	third, err := mysqlstore.AppendActivity(ctx, f.db, f.taskID, "step.done", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟归档造成游标有洞
	if _, err := f.db.Exec(`DELETE FROM activities WHERE task_id = ? AND seq = ?`, f.taskID, first.Seq+1); err != nil {
		t.Fatal(err)
	}

	all, err := mysqlstore.ListActivities(ctx, f.db, f.taskID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Seq != first.Seq || all[1].Seq != third.Seq {
		t.Fatalf("补拉事件 = %+v（游标必须容忍洞）", all)
	}
	afterFirst, err := mysqlstore.ListActivities(ctx, f.db, f.taskID, first.Seq, 100)
	if err != nil || len(afterFirst) != 1 || afterFirst[0].Kind != "step.done" {
		t.Fatalf("after=%d → %+v, err=%v", first.Seq, afterFirst, err)
	}
}

// 场景 4：Daemon 已完成但回调丢失，恢复先取证；证据不足时保持 reconciling，不盲目重跑。
func TestFaultLostDoneNoDuplicateExecution(t *testing.T) {
	f := newFaultFixture(t, domain.TaskExecuting, domain.StepExecuting)
	ctx := context.Background()
	attempt := f.insertAttempt(t, domain.AttemptDispatched)

	// 模拟租约过期后的状态：attempt unknown、Step reconciling（对账冻结）
	if _, err := f.db.Exec(`UPDATE attempts SET status = 'unknown' WHERE id = ?`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE plan_steps SET status = 'reconciling' WHERE id = ?`, f.stepID); err != nil {
		t.Fatal(err)
	}

	reconciler := &recovery.Reconciler{DB: f.db}
	verdict, _, err := reconciler.Reconcile(ctx, f.stepID)
	if err != nil {
		t.Fatal(err)
	}
	if verdict != recovery.VerdictUndecidable {
		t.Fatalf("无 git 证据时应转人工，实际 %q", verdict)
	}

	var attempts int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM attempts WHERE step_id = ?`, f.stepID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("对账不得盲目重跑: attempts=%d", attempts)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepReconciling {
		t.Fatalf("step = %q", step.Status)
	}
	got, _ := mysqlstore.GetAttemptByID(ctx, f.db, attempt.ID)
	if got.Status != domain.AttemptUnknown {
		t.Fatalf("attempt = %q", got.Status)
	}
}

// 场景 5：租约过期后旧执行回调到达，不推进新尝试。
func TestFaultOldExecutionCallbackIgnored(t *testing.T) {
	f := newFaultFixture(t, domain.TaskExecuting, domain.StepExecuting)
	ctx := context.Background()
	old := f.insertAttempt(t, domain.AttemptUnknown)
	latest := f.insertAttempt(t, domain.AttemptDispatched)

	payload, _ := json.Marshal(map[string]any{"attempt_no": old.AttemptNo, "commit_sha": "deadbeef"})
	env := protocol.Envelope{
		Type: protocol.TypeEvent, ExecutionID: old.ExecutionID,
		Payload: mustEvent(t, "step_done", payload),
	}
	handler := &orchestration.EventHandler{DB: f.db}
	if err := handler.Handle(ctx, "", env); err != nil {
		t.Fatal(err)
	}

	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepExecuting {
		t.Fatalf("旧执行推进了步骤: %q", step.Status)
	}
	got, _ := mysqlstore.GetAttemptByID(ctx, f.db, latest.ID)
	if got.Status != domain.AttemptDispatched {
		t.Fatalf("新尝试被旧回调修改: %q", got.Status)
	}
}

// 场景 6：取消与成功结果竞争，最终状态由事务与状态版本确定，任务不被迟到结果重新激活。
func TestFaultCancelBeatsLateSuccess(t *testing.T) {
	f := newFaultFixture(t, domain.TaskExecuting, domain.StepExecuting)
	ctx := context.Background()
	attempt := f.insertAttempt(t, domain.AttemptDispatched)

	canceller := &orchestration.Canceller{DB: f.db, Runtime: &faultRuntime{}}
	if _, allAcked, err := canceller.RequestCancel(ctx, f.taskID); err != nil || !allAcked {
		t.Fatalf("RequestCancel: %v, acked=%v", err, allAcked)
	}
	if err := canceller.FinalizeCancel(ctx, f.taskID, false); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{"attempt_no": attempt.AttemptNo, "commit_sha": "abc"})
	env := protocol.Envelope{
		Type: protocol.TypeEvent, ExecutionID: attempt.ExecutionID,
		Payload: mustEvent(t, "step_done", payload),
	}
	if err := (&orchestration.EventHandler{DB: f.db}).Handle(ctx, "", env); err != nil {
		t.Fatal(err)
	}

	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskCancelled {
		t.Fatalf("迟到成功复活了任务: %q", task.Status)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepCancelled {
		t.Fatalf("迟到成功改变了步骤: %q", step.Status)
	}
}

// 场景 8：上下文压缩保留需求约束与验收条件，摘要可定位原始证据。
func TestFaultContextBudgetKeepsConstraints(t *testing.T) {
	bundle := agentctx.Assemble(agentctx.Request{
		Constraints:  []string{"不得修改数据库结构"},
		Requirements: []string{"接口必须幂等", "错误码必须稳定"},
		Decisions: []agentctx.Decision{
			{Text: "使用 Redis 锁", EvidenceIDs: []string{"ev-42"}},
		},
		StepInput: "实现 X",
		Recent:    []string{"最近 1", "最近 2", "最近 3", "最近 4"},
		History:   []string{"历史 1", "历史 2", "历史 3", "历史 4"},
	})
	window := agentctx.EstimateTokens(agentctx.Assemble(agentctx.Request{
		Constraints:  []string{"不得修改数据库结构"},
		Requirements: []string{"接口必须幂等", "错误码必须稳定"},
		Decisions: []agentctx.Decision{
			{Text: "使用 Redis 锁", EvidenceIDs: []string{"ev-42"}},
		},
		StepInput: "实现 X",
		Recent:    []string{"最近 1"},
	}).Text())

	fitted, err := agentctx.Fit(bundle, window)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	text := fitted.Text()
	for _, must := range []string{
		"不得修改数据库结构", "接口必须幂等", "错误码必须稳定",
		"使用 Redis 锁（证据：ev-42）",
	} {
		if !contains(text, must) {
			t.Fatalf("丢失 0 级内容 %q:\n%s", must, text)
		}
	}
	if agentctx.EstimateTokens(text) > window {
		t.Fatalf("裁剪后超预算")
	}
	if len(fitted.Trimmed) == 0 {
		t.Fatal("应记录 budget.trimmed")
	}
}

func hasCode(err error, code errcode.Code) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

func mustEvent(t *testing.T, kind string, data []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(protocol.EventPayload{Kind: kind, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func contains(text, fragment string) bool {
	return strings.Contains(text, fragment)
}

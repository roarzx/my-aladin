//go:build integration

package orchestration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type stubRuntime struct {
	mu        sync.Mutex
	requests  []runtime.DispatchRequest
	cancelAck bool
}

func (s *stubRuntime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	return req.ExecutionID, nil
}

func (s *stubRuntime) Inspect(context.Context, string) (runtime.ExecutionSnapshot, error) {
	return runtime.ExecutionSnapshot{}, nil
}

func (s *stubRuntime) Cancel(context.Context, string) (bool, error) { return s.cancelAck, nil }

func (s *stubRuntime) Capabilities() runtime.CapabilitySet { return runtime.CapabilitySet{} }

func (s *stubRuntime) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type runnerFixture struct {
	db                   *sql.DB
	wsID, taskID, planID string
	step1, step2         string
}

func setupRunner(t *testing.T, agentOwner uint64, visibility agent.Visibility) *runnerFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &runnerFixture{
		db: db, wsID: idgen.New(), taskID: idgen.New(),
		planID: idgen.New(), step1: idgen.New(), step2: idgen.New(),
	}

	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "runner"); err != nil {
		t.Fatal(err)
	}

	agentID := idgen.New()
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: agentID, WorkspaceID: f.wsID, Name: "a", OwnerID: agentOwner,
		Visibility: visibility, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: idgen.New(), WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskExecuting,
		PlanBranch: "feature", ConfigSnapshot: json.RawMessage(`{}`),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, f.taskID, "a1", agentID, domain.TaskAgentPlanner); err != nil {
		t.Fatal(err)
	}

	if err := mysqlstore.InsertPlan(ctx, db, domain.Plan{
		ID: f.planID, TaskID: f.taskID, Revision: 1, ContentHash: "hash",
		ContentMD: "# plan", Status: domain.PlanExecuting,
	}); err != nil {
		t.Fatal(err)
	}
	for i, stepID := range []string{f.step1, f.step2} {
		if err := mysqlstore.InsertPlanStep(ctx, db, domain.Step{
			ID: stepID, PlanID: f.planID, Ordinal: i + 1,
			Title: "s", Body: "b", AgentRef: "a1", Status: domain.StepPending,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id IN (SELECT id FROM plan_steps WHERE plan_id = ?)`, f.planID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func TestRunOnceSerialDispatch(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	runtimeStub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: runtimeStub, Owner: "runner-1"}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if runtimeStub.count() != 1 {
		t.Fatalf("派发次数 = %d", runtimeStub.count())
	}

	steps, err := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Status != domain.StepExecuting || steps[1].Status != domain.StepPending {
		t.Fatalf("步骤状态 = %q/%q", steps[0].Status, steps[1].Status)
	}

	attempt, err := mysqlstore.GetAttemptByExecution(ctx, f.db, runtimeStub.requests[0].ExecutionID)
	if err != nil || attempt.AttemptNo != 1 || attempt.StepID != f.step1 {
		t.Fatalf("attempt = %+v, err=%v", attempt, err)
	}

	// 已有 executing 步骤时不得再派发
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	if runtimeStub.count() != 1 {
		t.Fatalf("串行约束失效，派发次数 = %d", runtimeStub.count())
	}

	// 完成第一步后派发第二步
	steps, _ = mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if _, err := mysqlstore.UpdateStepState(ctx, f.db, f.step1,
		domain.StepExecuting, domain.StepCompleted, steps[0].Version); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	if runtimeStub.count() != 2 {
		t.Fatalf("应派发第二步，派发次数 = %d", runtimeStub.count())
	}
}

func TestRunOnceRejectsUnschedulableAgent(t *testing.T) {
	f := setupRunner(t, 2, agent.Private) // 私有且属于用户 2，任务创建者是 1
	ctx := context.Background()
	runtimeStub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: runtimeStub, Owner: "runner-1"}

	err := runner.RunOnce(ctx, f.taskID)
	if err == nil {
		t.Fatal("应拒绝派发")
	}
	e, ok := errcode.As(err)
	if !ok || e.Code != errcode.AgentUnavailable {
		t.Fatalf("错误码 = %v", err)
	}
	if runtimeStub.count() != 0 {
		t.Fatal("未授权的 Agent 不应收到派发")
	}
	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepFailed {
		t.Fatalf("步骤状态 = %q", steps[0].Status)
	}
}

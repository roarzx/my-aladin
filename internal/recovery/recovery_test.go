//go:build integration

package recovery_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/recovery"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type fixture struct {
	db                   *sql.DB
	wsID, taskID, planID string
	stepID               string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &fixture{db: db, wsID: idgen.New(), taskID: idgen.New(), planID: idgen.New(), stepID: idgen.New()}

	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "recovery"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskExecuting,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
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
		AgentRef: "a1", Status: domain.StepReconciling,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertAttempt(ctx, db, domain.Attempt{
		ID: idgen.New(), StepID: f.stepID, AttemptNo: 1,
		ExecutionID: idgen.New(), LeaseEpoch: 1, Status: domain.AttemptUnknown,
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id IN (SELECT id FROM plan_steps WHERE plan_id = ?)`, f.planID)
		_, _ = db.Exec(`DELETE FROM context_entries WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

type staticSource struct {
	evidence recovery.Evidence
	err      error
}

func (s staticSource) Collect(context.Context, string) (recovery.Evidence, error) {
	return s.evidence, s.err
}

func TestReconcileCompleted(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	reconciler := &recovery.Reconciler{
		DB: f.db,
		Source: staticSource{evidence: recovery.Evidence{
			StepID: f.stepID, GitEvidenceKnown: true, NewCommits: true,
			SummaryPresent: true, SummaryDigestMatches: true,
			SummaryDigest: "d", CommitSHA: "c",
		}},
		Handlers: &orchestration.EventHandler{DB: f.db},
	}

	verdict, _, err := reconciler.Reconcile(ctx, f.stepID)
	if err != nil || verdict != recovery.VerdictCompleted {
		t.Fatalf("verdict=%q err=%v", verdict, err)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepCompleted {
		t.Fatalf("step = %q", step.Status)
	}
	attempt, _ := mysqlstore.GetLatestAttempt(ctx, f.db, f.stepID)
	if attempt.Status != domain.AttemptCompleted {
		t.Fatalf("attempt = %q", attempt.Status)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskChatting {
		t.Fatalf("task = %q（计划全部终态应进入 chatting）", task.Status)
	}
}

func TestReconcileFailed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	reconciler := &recovery.Reconciler{
		DB: f.db,
		Source: staticSource{evidence: recovery.Evidence{
			StepID: f.stepID, GitEvidenceKnown: true,
		}},
		Handlers: &orchestration.EventHandler{DB: f.db},
	}

	verdict, _, err := reconciler.Reconcile(ctx, f.stepID)
	if err != nil || verdict != recovery.VerdictFailed {
		t.Fatalf("verdict=%q err=%v", verdict, err)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepFailed {
		t.Fatalf("step = %q", step.Status)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskPaused {
		t.Fatalf("task = %q（失败应暂停）", task.Status)
	}
}

func TestReconcileUndecidableKeepsReconciling(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	reconciler := &recovery.Reconciler{
		DB:     f.db,
		Source: staticSource{evidence: recovery.Evidence{StepID: f.stepID}},
	}
	verdict, _, err := reconciler.Reconcile(ctx, f.stepID)
	if err != nil || verdict != recovery.VerdictUndecidable {
		t.Fatalf("verdict=%q err=%v", verdict, err)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepReconciling {
		t.Fatalf("step = %q，应保持 reconciling", step.Status)
	}
	activities, err := mysqlstore.ListActivities(ctx, f.db, f.taskID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, activity := range activities {
		if activity.Kind == "step.reconcile_pending" {
			found = true
		}
	}
	if !found {
		t.Fatal("应记录 reconcile_pending 审计事件")
	}
}

type fakeRunner struct {
	mu  sync.Mutex
	ids []string
}

func (f *fakeRunner) RunOnce(_ context.Context, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, taskID)
	return nil
}

func TestWatchdogSweep(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`UPDATE plan_steps SET status = 'executing' WHERE id = ?`, f.stepID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE attempts SET status = 'dispatched' WHERE step_id = ?`, f.stepID); err != nil {
		t.Fatal(err)
	}

	lease := &orchestration.Lease{
		DB: f.db, Store: redisstore.NewLeaseStore(testkit.NewRedis(t)),
		TTL: 300 * time.Millisecond,
	}
	runner := &fakeRunner{}
	watchdog := &recovery.Watchdog{DB: f.db, Runner: runner, Lease: lease, Log: slog.Default()}

	result, err := watchdog.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// 集成库可能被其它测试包并发写入，这里只断言本轮任务被处理。
	if result.Tasks < 1 || result.Advanced < 1 {
		t.Fatalf("执行中任务扫描 = %+v", result)
	}
	found := false
	for _, id := range runner.ids {
		if id == f.taskID {
			found = true
		}
	}
	if !found {
		t.Fatalf("Runner 未处理本轮任务，调用 = %+v", runner.ids)
	}
	if result.Expired < 1 {
		t.Fatalf("过期租约 = %+v", result)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepReconciling {
		t.Fatalf("step = %q", step.Status)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskPaused {
		t.Fatalf("task = %q（租约超时应暂停等待对账）", task.Status)
	}
}

func TestApplyManualVerdict(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	reconciler := &recovery.Reconciler{DB: f.db}
	if err := reconciler.ApplyVerdict(ctx, f.stepID, recovery.VerdictCompleted, "人工确认提交已推送"); err != nil {
		t.Fatalf("ApplyVerdict: %v", err)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.stepID)
	if step.Status != domain.StepCompleted {
		t.Fatalf("step = %q", step.Status)
	}
	var verdict sql.NullString
	if err := f.db.QueryRowContext(ctx,
		`SELECT reconcile_verdict FROM attempts WHERE step_id = ?`, f.stepID).Scan(&verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.String != "completed" {
		t.Fatalf("reconcile_verdict = %q", verdict.String)
	}
}

//go:build integration

package plan_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/plan"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fixture struct {
	db     *sql.DB
	wsID   string
	taskID string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &fixture{db: db, wsID: idgen.New(), taskID: idgen.New()}

	mustExec(t, db, `INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "plan")
	planner := agent.Agent{
		ID: idgen.New(), WorkspaceID: f.wsID, Name: "planner", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: idgen.New(), WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}
	member := planner
	member.ID = idgen.New()
	member.Name = "member"
	for _, a := range []agent.Agent{planner, member} {
		if err := mysqlstore.CreateAgent(ctx, db, a); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	task := domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskPending,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}
	if err := mysqlstore.InsertTask(ctx, db, task); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, f.taskID, "a1", planner.ID, domain.TaskAgentPlanner); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, f.taskID, "a2", member.ID, domain.TaskAgentMember); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM confirmations WHERE plan_id IN (SELECT id FROM plans WHERE task_id = ?)`, f.taskID)
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id IN (SELECT id FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE task_id = ?))`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("执行 SQL: %v", err)
	}
}

func (f *fixture) proposal(revision int) plan.Proposal {
	return plan.Proposal{
		ContentMD: "# 计划",
		Revision:  revision,
		Steps: []plan.StepInput{
			{Ordinal: 1, Title: "第一步", Body: "body", AgentRef: "a1"},
			{Ordinal: 2, Title: "第二步", Body: "body", AgentRef: "a2"},
		},
	}
}

func TestProposeConfirmStart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := &plan.Service{DB: f.db}

	p1, steps, err := svc.Propose(ctx, f.taskID, f.proposal(1))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if p1.Revision != 1 || len(steps) != 2 {
		t.Fatalf("plan=%+v steps=%d", p1, len(steps))
	}
	task, err := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if err != nil || task.Status != domain.TaskPlanConfirming {
		t.Fatalf("任务状态 = %q, err=%v", task.Status, err)
	}

	// 旧 revision 重提 → 冲突
	if _, _, err := svc.Propose(ctx, f.taskID, f.proposal(1)); !hasCode(err, errcode.RevisionConflict) {
		t.Fatalf("期望 REVISION_CONFLICT，实际: %v", err)
	}

	p2, _, err := svc.Propose(ctx, f.taskID, f.proposal(2))
	if err != nil {
		t.Fatalf("Propose v2: %v", err)
	}
	old, _, err := mysqlstore.GetPlanByRevision(ctx, f.db, f.taskID, 1)
	if err != nil || old.ID != p1.ID {
		t.Fatalf("旧版本应保留: %+v, err=%v", old, err)
	}

	// 旧版本确认失效
	if _, err := svc.Confirm(ctx, p1.ID, 1, p1.ContentHash, 1); !hasCode(err, errcode.PlanSuperseded) {
		t.Fatalf("期望 PLAN_SUPERSEDED，实际: %v", err)
	}
	// hash 不匹配
	if _, err := svc.Confirm(ctx, p2.ID, 2, "deadbeef", 1); !hasCode(err, errcode.HashMismatch) {
		t.Fatalf("期望 HASH_MISMATCH，实际: %v", err)
	}
	confirmation, err := svc.Confirm(ctx, p2.ID, 2, p2.ContentHash, 1)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	// 重复确认幂等
	again, err := svc.Confirm(ctx, p2.ID, 2, p2.ContentHash, 1)
	if err != nil || again.ID != confirmation.ID {
		t.Fatalf("重复确认不幂等: %+v, err=%v", again, err)
	}

	task, _ = mysqlstore.GetTask(ctx, f.db, f.taskID)
	started, err := svc.Start(ctx, f.taskID, task.Version)
	if err != nil || started.Status != domain.TaskExecuting {
		t.Fatalf("Start: %+v, err=%v", started, err)
	}
	// D13：启动时分配计划分支（aladdin/<taskid 前 8 位>），且可重复读取
	wantBranch := "aladdin/" + strings.ReplaceAll(f.taskID, "-", "")[:8]
	if started.PlanBranch != wantBranch {
		t.Fatalf("PlanBranch = %q, 期望 %q", started.PlanBranch, wantBranch)
	}
	persisted, err := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if err != nil || persisted.PlanBranch != wantBranch {
		t.Fatalf("持久化 PlanBranch = %q, err=%v", persisted.PlanBranch, err)
	}
	// 再次启动 → 冲突
	if _, err := svc.Start(ctx, f.taskID, started.Version); !hasCode(err, errcode.Conflict) {
		t.Fatalf("期望 CONFLICT，实际: %v", err)
	}
}

func TestStartRequiresConfirmation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := &plan.Service{DB: f.db}

	if _, _, err := svc.Propose(ctx, f.taskID, f.proposal(1)); err != nil {
		t.Fatal(err)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if _, err := svc.Start(ctx, f.taskID, task.Version); !hasCode(err, errcode.NotConfirmed) {
		t.Fatalf("期望 NOT_CONFIRMED，实际: %v", err)
	}
}

func TestProposeRejectsBadAgentRef(t *testing.T) {
	f := setup(t)
	req := f.proposal(1)
	req.Steps[0].AgentRef = "nope"
	_, _, err := (&plan.Service{DB: f.db}).Propose(context.Background(), f.taskID, req)
	if !hasCode(err, errcode.AgentRefInvalid) {
		t.Fatalf("期望 AGENT_REF_INVALID，实际: %v", err)
	}
}

func TestProposeRevisionConflict(t *testing.T) {
	f := setup(t)
	_, _, err := (&plan.Service{DB: f.db}).Propose(context.Background(), f.taskID, f.proposal(5))
	if !hasCode(err, errcode.RevisionConflict) {
		t.Fatalf("期望 REVISION_CONFLICT，实际: %v", err)
	}
}

func hasCode(err error, code errcode.Code) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	e, ok := errcode.As(err)
	return ok && e.Code == code
}

//go:build integration

package orchestration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/session"
	"github.com/roarzx/my-aladin/internal/squad"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type squadFakeDispatcher struct {
	mu    sync.Mutex
	calls []orchestration.SquadDispatch
	fail  bool
}

func (d *squadFakeDispatcher) DispatchSquad(_ context.Context, req orchestration.SquadDispatch) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, req)
	if d.fail {
		d.fail = false
		return errors.New("handoff failed")
	}
	return nil
}

func (d *squadFakeDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

type squadFixture struct {
	db                    *sql.DB
	wsID, taskID, squadID string
	leaderID, memberID    string
	item1, item2          string
}

func setupSquad(t *testing.T) *squadFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &squadFixture{
		db: db, wsID: idgen.New(), taskID: idgen.New(), squadID: idgen.New(),
		leaderID: idgen.New(), memberID: idgen.New(), item1: idgen.New(), item2: idgen.New(),
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "squad-runner"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{f.leaderID, f.memberID} {
		if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
			ID: id, WorkspaceID: f.wsID, Name: id[:8], OwnerID: 1,
			Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: "machine-squad", WorkDir: "/tmp/work", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mysqlstore.CreateSquad(ctx, db, domain.Squad{
		ID: f.squadID, WorkspaceID: f.wsID, Name: "squad-" + f.squadID[:8],
		LeaderAgentID: f.leaderID, MemberIDs: []string{f.memberID},
		Instructions: "协作", Version: 1, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeSquad, Status: domain.TaskExecuting,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := squad.FreezeTx(ctx, db, f.squadID, f.taskID); err != nil {
		t.Fatal(err)
	}
	for i, itemID := range []string{f.item1, f.item2} {
		if err := mysqlstore.InsertWorkItem(ctx, db, domain.WorkItem{
			ID: itemID, TaskID: f.taskID, Ordinal: i + 1,
			OwnerAgentID: f.leaderID, Status: domain.WorkItemReady,
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM work_items WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM squad_snapshots WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM squads WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func TestSquadRunner(t *testing.T) {
	f := setupSquad(t)
	ctx := context.Background()
	dispatcher := &squadFakeDispatcher{fail: true}
	runner := &orchestration.SquadRunner{
		DB: f.db, Dispatcher: dispatcher, Sessions: &session.Service{DB: f.db},
		Owner: "squad-runner",
	}

	// 第一次交接失败 → 工作项 blocked
	if err := runner.RunOnce(ctx, f.taskID); err == nil {
		t.Fatal("交接失败应返回错误")
	}
	item1, _ := mysqlstore.GetWorkItem(ctx, f.db, f.item1)
	if item1.Status != domain.WorkItemBlocked {
		t.Fatalf("item1 = %q", item1.Status)
	}
	item2, _ := mysqlstore.GetWorkItem(ctx, f.db, f.item2)
	if item2.Status != domain.WorkItemReady {
		t.Fatalf("item2 = %q（不得并发推进）", item2.Status)
	}

	// 定向重试交接成功
	if err := runner.RetryHandoff(ctx, f.item1); err != nil {
		t.Fatalf("RetryHandoff: %v", err)
	}
	item1, _ = mysqlstore.GetWorkItem(ctx, f.db, f.item1)
	if item1.Status != domain.WorkItemRunning {
		t.Fatalf("重试后 item1 = %q", item1.Status)
	}

	// 有 running 时不再推进第二个
	before := dispatcher.count()
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.count() != before {
		t.Fatalf("running 期间仍派发: %d → %d", before, dispatcher.count())
	}
	item2, _ = mysqlstore.GetWorkItem(ctx, f.db, f.item2)
	if item2.Status != domain.WorkItemReady {
		t.Fatalf("item2 = %q", item2.Status)
	}
}

func TestRunnerMutualExclusion(t *testing.T) {
	f := setupSquad(t)
	ctx := context.Background()
	client := testkit.NewRedis(t)
	dispatcher := &squadFakeDispatcher{}
	runner := &orchestration.SquadRunner{
		DB: f.db, Dispatcher: dispatcher,
		Lock: orchestration.NewTaskLock(client, time.Second, -1), Owner: "squad",
	}

	// 另一个 Runner（PlanRunner 同等）持有同一任务锁 → 本 Runner 必须跳过
	lock := orchestration.NewTaskLock(client, time.Second, -1)
	release, err := lock.Acquire(ctx, f.taskID, "plan")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if dispatcher.count() != 0 {
		t.Fatalf("锁被占用时仍派发: %d", dispatcher.count())
	}
	item1, _ := mysqlstore.GetWorkItem(ctx, f.db, f.item1)
	if item1.Status != domain.WorkItemReady {
		t.Fatalf("锁被占用时工作项被推进: %q", item1.Status)
	}
}

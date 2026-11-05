//go:build integration

package session_test

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
	"github.com/roarzx/my-aladin/internal/machine"
	"github.com/roarzx/my-aladin/internal/session"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type fakeDispatcher struct {
	mu    sync.Mutex
	calls []session.PlanningDispatch
	err   error
}

func (f *fakeDispatcher) DispatchPlanning(_ context.Context, req session.PlanningDispatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	return f.err
}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fixture struct {
	db                    *sql.DB
	wsID, taskID, agentID string
	machineID             string
}

func setupSession(t *testing.T, status machine.Status) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &fixture{
		db: db, wsID: idgen.New(), taskID: idgen.New(),
		agentID: idgen.New(), machineID: idgen.New(),
	}

	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "sessions"); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.UpsertMachine(ctx, db, machine.Machine{
		ID: f.machineID, WorkspaceID: f.wsID, Name: "m", Kind: machine.KindLocal,
		Status: status, Capabilities: []machine.Capability{machine.CapGit},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: f.agentID, WorkspaceID: f.wsID, Name: "agent", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: f.machineID, WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskPending,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertTaskAgent(ctx, db, f.taskID, "a1", f.agentID, domain.TaskAgentPlanner); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM machines WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func sessionState(t *testing.T, f *fixture) string {
	t.Helper()
	record, err := mysqlstore.FindSessionByKind(context.Background(), f.db, f.taskID, f.agentID, "planning")
	if err != nil {
		t.Fatalf("读取会话: %v", err)
	}
	return record.DispatchState
}

func TestDispatchPlanningOffline(t *testing.T) {
	f := setupSession(t, machine.StatusOffline)
	dispatcher := &fakeDispatcher{}
	svc := &session.Service{DB: f.db, Dispatcher: dispatcher}

	if err := svc.DispatchPlanning(context.Background(), f.taskID, "prompt"); err != nil {
		t.Fatalf("机器离线不应报错: %v", err)
	}
	if got := sessionState(t, f); got != "waiting_agent" {
		t.Fatalf("dispatch_state = %q", got)
	}
	if dispatcher.count() != 0 {
		t.Fatal("离线时不应下发")
	}
}

func TestDispatchPlanningOnline(t *testing.T) {
	f := setupSession(t, machine.StatusOnline)
	dispatcher := &fakeDispatcher{}
	svc := &session.Service{DB: f.db, Dispatcher: dispatcher}

	start := time.Now()
	if err := svc.DispatchPlanning(context.Background(), f.taskID, "prompt"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("在线派发耗时 %v，超过 200ms", elapsed)
	}
	if got := sessionState(t, f); got != "dispatched" {
		t.Fatalf("dispatch_state = %q", got)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("下发次数 = %d", dispatcher.count())
	}
	req := dispatcher.calls[0]
	if req.TaskID != f.taskID || req.AgentID != f.agentID || req.MachineID != f.machineID || req.Prompt != "prompt" {
		t.Fatalf("请求 = %+v", req)
	}
}

func TestDispatchPlanningRejected(t *testing.T) {
	f := setupSession(t, machine.StatusOnline)
	dispatcher := &fakeDispatcher{err: errors.New("runtime not running")}
	svc := &session.Service{DB: f.db, Dispatcher: dispatcher}

	if err := svc.DispatchPlanning(context.Background(), f.taskID, "prompt"); err != nil {
		t.Fatalf("派发被拒不应返回错误: %v", err)
	}
	if got := sessionState(t, f); got != "dispatch_failed" {
		t.Fatalf("dispatch_state = %q", got)
	}
}

func TestDispatchPlanningWithoutDispatcher(t *testing.T) {
	f := setupSession(t, machine.StatusOnline)
	svc := &session.Service{DB: f.db}

	if err := svc.DispatchPlanning(context.Background(), f.taskID, "prompt"); err != nil {
		t.Fatal(err)
	}
	if got := sessionState(t, f); got != "waiting_agent" {
		t.Fatalf("未接入 Dispatcher 时不应假装已派发，实际 %q", got)
	}
}

func TestEnsureSessionIdempotent(t *testing.T) {
	f := setupSession(t, machine.StatusOffline)
	svc := &session.Service{DB: f.db}
	first, err := svc.Ensure(context.Background(), f.taskID, f.agentID, "planning")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Ensure(context.Background(), f.taskID, f.agentID, "planning")
	if err != nil || first.ID != second.ID {
		t.Fatalf("会话不幂等: %q vs %q, err=%v", first.ID, second.ID, err)
	}
}

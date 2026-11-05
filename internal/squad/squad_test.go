//go:build integration

package squad_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/squad"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fixture struct {
	db         *sql.DB
	wsID       string
	leaderID   string
	memberID   string
	squadID    string
	taskID     string
	workItemID string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &fixture{
		db: db, wsID: idgen.New(), leaderID: idgen.New(), memberID: idgen.New(),
		squadID: idgen.New(), taskID: idgen.New(), workItemID: idgen.New(),
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "squad"); err != nil {
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
		ApprovalPolicy: domain.ApprovalPolicy{Required: true, Chain: []domain.ApprovalStep{
			{Order: 1, ApproverID: 10}, {Order: 2, ApproverID: 20},
		}},
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
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM approvals WHERE proposal_id IN (SELECT id FROM proposals WHERE work_item_id IN (SELECT id FROM work_items WHERE task_id = ?))`, f.taskID)
		_, _ = db.Exec(`DELETE FROM proposals WHERE work_item_id IN (SELECT id FROM work_items WHERE task_id = ?)`, f.taskID)
		_, _ = db.Exec(`DELETE FROM work_items WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM squad_snapshots WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM squads WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func TestFreeze(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	svc := &squad.Service{DB: f.db}

	snap, err := svc.Freeze(ctx, f.squadID, f.taskID)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if snap.LeaderAgentID != f.leaderID || len(snap.MemberIDs) != 1 ||
		len(snap.ApprovalPolicy.Chain) != 2 {
		t.Fatalf("快照 = %+v", snap)
	}

	// 修改小队定义不影响已冻结快照
	def, _ := mysqlstore.GetSquad(ctx, f.db, f.squadID)
	def.Instructions = "新的协作指令"
	def.LeaderAgentID = f.memberID
	if err := mysqlstore.UpdateSquad(ctx, f.db, def, def.Version); err != nil {
		t.Fatalf("UpdateSquad: %v", err)
	}
	def.Version++
	frozen, err := mysqlstore.GetSquadSnapshot(ctx, f.db, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.LeaderAgentID != f.leaderID || frozen.Instructions != "协作" {
		t.Fatalf("快照被后续修改影响: %+v", frozen)
	}

	// 幂等：同一任务再次冻结返回同一快照
	again, err := svc.Freeze(ctx, f.squadID, f.taskID)
	if err != nil || again.ID != snap.ID {
		t.Fatalf("重复冻结 = %+v, err=%v", again, err)
	}

	// 停用后不能冻结到新任务
	def.Enabled = false
	if err := mysqlstore.UpdateSquad(ctx, f.db, def, def.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Freeze(ctx, f.squadID, idgen.New()); err == nil {
		t.Fatal("停用小队不应冻结")
	}
}

func TestApprovalChain(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := (&squad.Service{DB: f.db}).Freeze(ctx, f.squadID, f.taskID); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertWorkItem(ctx, f.db, domain.WorkItem{
		ID: f.workItemID, TaskID: f.taskID, Ordinal: 1,
		OwnerAgentID: f.leaderID, Status: domain.WorkItemProposed,
	}); err != nil {
		t.Fatal(err)
	}

	proposal, err := squad.Propose(ctx, f.db, f.workItemID, "方案 v1")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if proposal.Revision != 1 || proposal.ContentHash == "" {
		t.Fatalf("proposal = %+v", proposal)
	}
	item, _ := mysqlstore.GetWorkItem(ctx, f.db, f.workItemID)
	if item.Status != domain.WorkItemAwaitingApproval {
		t.Fatalf("状态 = %q", item.Status)
	}

	// 跳级被拒
	if err := squad.Approve(ctx, f.db, proposal.ID, 1, 20, 2); !hasCode(err, errcode.Conflict) {
		t.Fatalf("跳级应失败，实际: %v", err)
	}
	// 越权被拒
	if err := squad.Approve(ctx, f.db, proposal.ID, 1, 20, 1); !hasCode(err, errcode.Forbidden) {
		t.Fatalf("越权应失败，实际: %v", err)
	}
	// 顺序审批
	if err := squad.Approve(ctx, f.db, proposal.ID, 1, 10, 1); err != nil {
		t.Fatalf("第一级审批: %v", err)
	}
	item, _ = mysqlstore.GetWorkItem(ctx, f.db, f.workItemID)
	if item.Status != domain.WorkItemAwaitingApproval {
		t.Fatalf("未全部通过时状态 = %q", item.Status)
	}
	if err := squad.Approve(ctx, f.db, proposal.ID, 1, 20, 2); err != nil {
		t.Fatalf("第二级审批: %v", err)
	}
	item, _ = mysqlstore.GetWorkItem(ctx, f.db, f.workItemID)
	if item.Status != domain.WorkItemReady {
		t.Fatalf("全部通过后状态 = %q", item.Status)
	}

	// 修订后旧审批不能授权新方案
	rev2, err := squad.Propose(ctx, f.db, f.workItemID, "方案 v2")
	if err != nil || rev2.Revision != 2 {
		t.Fatalf("rev2 = %+v, err=%v", rev2, err)
	}
	if err := squad.Approve(ctx, f.db, proposal.ID, 1, 10, 1); !hasCode(err, errcode.PlanSuperseded) {
		t.Fatalf("旧方案审批应被取代，实际: %v", err)
	}
}

func TestStartWorkItemSingleRunning(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	secondID := idgen.New()
	for _, item := range []domain.WorkItem{
		{ID: f.workItemID, TaskID: f.taskID, Ordinal: 1, OwnerAgentID: f.leaderID, Status: domain.WorkItemReady},
		{ID: secondID, TaskID: f.taskID, Ordinal: 2, OwnerAgentID: f.memberID, Status: domain.WorkItemReady},
	} {
		if err := mysqlstore.InsertWorkItem(ctx, f.db, item); err != nil {
			t.Fatal(err)
		}
	}

	first, err := mysqlstore.StartWorkItem(ctx, f.db, f.workItemID)
	if err != nil || first.Status != domain.WorkItemRunning {
		t.Fatalf("StartWorkItem: %+v, err=%v", first, err)
	}
	if _, err := mysqlstore.StartWorkItem(ctx, f.db, secondID); !errors.Is(err, mysqlstore.ErrWorkItemRunning) {
		t.Fatalf("第二个 running 应被拒绝，实际: %v", err)
	}

	if _, err := mysqlstore.UpdateWorkItemStatus(ctx, f.db, f.workItemID,
		domain.WorkItemRunning, domain.WorkItemDone, first.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.StartWorkItem(ctx, f.db, secondID); err != nil {
		t.Fatalf("前一个结束后应可启动: %v", err)
	}
}

func hasCode(err error, code errcode.Code) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

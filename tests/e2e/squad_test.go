//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/session"
	"github.com/roarzx/my-aladin/internal/squad"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"gopkg.in/yaml.v3"
)

type squadScenario struct {
	Name  string `yaml:"name"`
	Squad struct {
		Name         string   `yaml:"name"`
		Instructions string   `yaml:"instructions"`
		Approvers    []uint64 `yaml:"approvers"`
	} `yaml:"squad"`
	Tasks []struct {
		ID    string `yaml:"id"`
		Title string `yaml:"title"`
	} `yaml:"tasks"`
}

type scenarioDispatcher struct{ calls int }

func (d *scenarioDispatcher) DispatchSquad(context.Context, orchestration.SquadDispatch) error {
	d.calls++
	return nil
}

func loadSquadScenario(t *testing.T, path string) squadScenario {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var scenario squadScenario
	if err := yaml.Unmarshal(raw, &scenario); err != nil {
		t.Fatalf("解析场景: %v", err)
	}
	return scenario
}

func TestSquad(t *testing.T) {
	scenario := loadSquadScenario(t, "../fixtures/squads/approval_chain.yaml")
	if len(scenario.Tasks) < 2 {
		t.Fatal("场景至少需要两个任务")
	}

	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	wsID := idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "squad-e2e"); err != nil {
		t.Fatal(err)
	}
	leaderID, memberID := idgen.New(), idgen.New()
	for _, id := range []string{leaderID, memberID} {
		if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
			ID: id, WorkspaceID: wsID, Name: id[:8], OwnerID: 1,
			Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: "machine-e2e", WorkDir: "/tmp/e2e", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	squadID := idgen.New()
	chain := make([]domain.ApprovalStep, 0, len(scenario.Squad.Approvers))
	for i, approver := range scenario.Squad.Approvers {
		chain = append(chain, domain.ApprovalStep{Order: i + 1, ApproverID: approver})
	}
	if err := mysqlstore.CreateSquad(ctx, db, domain.Squad{
		ID: squadID, WorkspaceID: wsID, Name: scenario.Squad.Name,
		LeaderAgentID: leaderID, MemberIDs: []string{memberID},
		Instructions: scenario.Squad.Instructions, Version: 1, Enabled: true,
		ApprovalPolicy: domain.ApprovalPolicy{Required: true, Chain: chain},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := (&task.Service{DB: db}).Create(ctx, 1, wsID, task.CreateRequest{
		Name: "小队 E2E", Mode: domain.ModeSquad, SquadID: squadID,
	})
	if err != nil {
		t.Fatalf("创建小队任务: %v", err)
	}
	// 让任务进入 executing（小队 Runner 的前提）
	if _, err := db.Exec(`UPDATE tasks SET status = 'executing' WHERE id = ?`, created.ID); err != nil {
		t.Fatal(err)
	}
	items := make([]string, 0, len(scenario.Tasks))
	for i := range scenario.Tasks {
		itemID := idgen.New()
		if err := mysqlstore.InsertWorkItem(ctx, db, domain.WorkItem{
			ID: itemID, TaskID: created.ID, Ordinal: i + 1,
			OwnerAgentID: leaderID, Status: domain.WorkItemProposed,
		}); err != nil {
			t.Fatal(err)
		}
		items = append(items, itemID)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM approvals WHERE proposal_id IN (SELECT id FROM proposals WHERE work_item_id IN (SELECT id FROM work_items WHERE task_id = ?))`, created.ID)
		_, _ = db.Exec(`DELETE FROM proposals WHERE work_item_id IN (SELECT id FROM work_items WHERE task_id = ?)`, created.ID)
		_, _ = db.Exec(`DELETE FROM work_items WHERE task_id = ?`, created.ID)
		_, _ = db.Exec(`DELETE FROM squad_snapshots WHERE task_id = ?`, created.ID)
		_, _ = db.Exec(`DELETE FROM squads WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id = ?`, created.ID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, created.ID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})

	// 方案 → 多级审批
	proposal, err := squad.Propose(ctx, db, items[0], "方案 v1: "+scenario.Tasks[0].Title)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if err := squad.Approve(ctx, db, proposal.ID, 1, scenario.Squad.Approvers[1], 2); !hasCode(err, errcode.Conflict) {
		t.Fatalf("跳级审批应失败，实际: %v", err)
	}
	if err := squad.Approve(ctx, db, proposal.ID, 1, scenario.Squad.Approvers[0], 1); err != nil {
		t.Fatalf("第一级审批: %v", err)
	}

	// 修订方案 → 旧审批不能继续授权
	rev2, err := squad.Propose(ctx, db, items[0], "方案 v2")
	if err != nil {
		t.Fatalf("修订方案: %v", err)
	}
	if err := squad.Approve(ctx, db, proposal.ID, 1, scenario.Squad.Approvers[1], 2); !hasCode(err, errcode.PlanSuperseded) {
		t.Fatalf("旧方案审批应失效，实际: %v", err)
	}
	for i, approver := range scenario.Squad.Approvers {
		if err := squad.Approve(ctx, db, rev2.ID, 2, approver, i+1); err != nil {
			t.Fatalf("修订后第 %d 级审批: %v", i+1, err)
		}
	}
	item, _ := mysqlstore.GetWorkItem(ctx, db, items[0])
	if item.Status != domain.WorkItemReady {
		t.Fatalf("审批后 = %q", item.Status)
	}

	// SquadRunner 启动第一个工作项
	dispatcher := &scenarioDispatcher{}
	runner := &orchestration.SquadRunner{
		DB: db, Dispatcher: dispatcher, Sessions: &session.Service{DB: db}, Owner: "e2e-squad",
	}
	if err := runner.RunOnce(ctx, created.ID); err != nil {
		t.Fatalf("SquadRunner: %v", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("派发次数 = %d", dispatcher.calls)
	}
	item, _ = mysqlstore.GetWorkItem(ctx, db, items[0])
	if item.Status != domain.WorkItemRunning {
		t.Fatalf("工作项 = %q", item.Status)
	}
	// 同一 Issue 不并发推进第二个
	if err := runner.RunOnce(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("并发推进了第二个工作项: %d", dispatcher.calls)
	}
	if _, err := mysqlstore.UpdateWorkItemStatus(ctx, db, items[0],
		domain.WorkItemRunning, domain.WorkItemDone, item.Version); err != nil {
		t.Fatal(err)
	}

	// 第二项走完审批后由 Runner 接续
	proposal2, err := squad.Propose(ctx, db, items[1], "方案 v1: "+scenario.Tasks[1].Title)
	if err != nil {
		t.Fatal(err)
	}
	for i, approver := range scenario.Squad.Approvers {
		if err := squad.Approve(ctx, db, proposal2.ID, 1, approver, i+1); err != nil {
			t.Fatalf("第二项审批: %v", err)
		}
	}
	if err := runner.RunOnce(ctx, created.ID); err != nil {
		t.Fatalf("RunOnce 第二项: %v", err)
	}
	if dispatcher.calls != 2 {
		t.Fatalf("第二项未派发: %d", dispatcher.calls)
	}
}

func hasCode(err error, code errcode.Code) bool {
	var coded *errcode.Error
	return errors.As(err, &coded) && coded.Code == code
}

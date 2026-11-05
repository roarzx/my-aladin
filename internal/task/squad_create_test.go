//go:build integration

package task_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestCreateSquadTask(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	wsID := idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "squad-create"); err != nil {
		t.Fatal(err)
	}
	leaderID, memberID := idgen.New(), idgen.New()
	for _, id := range []string{leaderID, memberID} {
		if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
			ID: id, WorkspaceID: wsID, Name: id[:8], OwnerID: 1,
			Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: "m", WorkDir: "/tmp/work", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	squadID := idgen.New()
	if err := mysqlstore.CreateSquad(ctx, db, domain.Squad{
		ID: squadID, WorkspaceID: wsID, Name: "squad-" + squadID[:8],
		LeaderAgentID: leaderID, MemberIDs: []string{memberID},
		Instructions: "协作", Version: 1, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM squad_snapshots WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM task_agents WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM squads WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})

	svc := &task.Service{DB: db}
	created, err := svc.Create(ctx, 1, wsID, task.CreateRequest{
		Name: "小队任务", Mode: domain.ModeSquad, SquadID: squadID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Mode != domain.ModeSquad {
		t.Fatalf("mode = %q", created.Mode)
	}

	snap, err := mysqlstore.GetSquadSnapshot(ctx, db, created.ID)
	if err != nil || snap.SquadID != squadID || snap.LeaderAgentID != leaderID {
		t.Fatalf("快照 = %+v, err=%v", snap, err)
	}
	plannerID, err := mysqlstore.GetPlannerAgentID(ctx, db, created.ID)
	if err != nil || plannerID != leaderID {
		t.Fatalf("planner = %q, err=%v", plannerID, err)
	}
	var members int
	if err := db.QueryRow(`SELECT COUNT(*) FROM task_agents WHERE task_id = ?`, created.ID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 2 {
		t.Fatalf("任务成员 = %d", members)
	}
}

func TestCreateSquadTaskRejectsMutualExclusion(t *testing.T) {
	// 请求级校验已在 validate_test 覆盖；这里确认服务层不会绕过
	_, err := (&task.Service{}).Create(context.Background(), 1, "ws", task.CreateRequest{
		Name: "x", Mode: domain.ModeSquad, SquadID: "s1",
		AgentIDs: []string{"a1"}, PlannerAgentID: "a1",
	})
	if err == nil {
		t.Fatal("squadId 与 agentIds 互斥，应拒绝")
	}
}

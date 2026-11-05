//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type squadCLIFixture struct {
	db          *sql.DB
	wsID        string
	actor       uint64
	agentID     string
	secondAgent string
	taskID      string
	workItemID  string
	proposalID  string
}

func setupSquadCLI(t *testing.T) *squadCLIFixture {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	f := &squadCLIFixture{
		db: db, wsID: idgen.New(), actor: 1, agentID: idgen.New(), secondAgent: idgen.New(),
		taskID: idgen.New(), workItemID: idgen.New(), proposalID: idgen.New(),
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, f.wsID, "squad-cli"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{f.agentID, f.secondAgent} {
		if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
			ID: id, WorkspaceID: f.wsID, Name: id[:8], OwnerID: 1,
			Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: "m", WorkDir: "/tmp/work", SystemPrompt: "p",
			CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: f.taskID, WorkspaceID: f.wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeSquad, Status: domain.TaskExecuting,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertWorkItem(ctx, db, domain.WorkItem{
		ID: f.workItemID, TaskID: f.taskID, Ordinal: 1,
		OwnerAgentID: f.agentID, Status: domain.WorkItemReady,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.InsertProposal(ctx, db, mysqlstore.Proposal{
		ID: f.proposalID, WorkItemID: f.workItemID, Revision: 1,
		Content: "你好世界abc", ContentHash: "hash",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.PutContextVersion(ctx, db, f.taskID, "agent", f.agentID, "agent_memory", "记忆", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM context_entries WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM proposals WHERE work_item_id = ?`, f.workItemID)
		_, _ = db.Exec(`DELETE FROM work_items WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, f.taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, f.wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, f.wsID)
	})
	return f
}

func TestSquadCommands(t *testing.T) {
	f := setupSquadCLI(t)
	ctx := context.Background()
	var out bytes.Buffer

	if err := squadAction(ctx, f.db, f.workItemID, "start", f.actor, &out); err != nil {
		t.Fatalf("action start: %v", err)
	}
	if !strings.Contains(out.String(), "running") {
		t.Fatalf("start 输出 = %q", out.String())
	}

	out.Reset()
	if err := squadHandoff(ctx, f.db, f.workItemID, f.secondAgent, &out); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if !strings.Contains(out.String(), "->") {
		t.Fatalf("handoff 输出 = %q", out.String())
	}
	item, _ := mysqlstore.GetWorkItem(ctx, f.db, f.workItemID)
	if item.OwnerAgentID != f.secondAgent {
		t.Fatalf("owner = %q", item.OwnerAgentID)
	}

	out.Reset()
	if err := squadAction(ctx, f.db, f.workItemID, "complete", f.actor, &out); err != nil {
		t.Fatalf("action complete: %v", err)
	}
	if !strings.Contains(out.String(), "done") {
		t.Fatalf("complete 输出 = %q", out.String())
	}

	out.Reset()
	if err := squadContext(ctx, f.db, f.taskID, f.agentID, &out); err != nil {
		t.Fatalf("context: %v", err)
	}
	if !strings.Contains(out.String(), "agent_memory") || !strings.Contains(out.String(), "记忆") {
		t.Fatalf("context 输出 = %q", out.String())
	}

	out.Reset()
	if err := squadArtifact(ctx, f.db, f.proposalID, 0, 6, &out); err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if out.String() != "你好" {
		t.Fatalf("artifact 输出 = %q", out.String())
	}
}

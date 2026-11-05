//go:build integration

package session_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/session"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func setupSquadSession(t *testing.T) (*sql.DB, string, string, string) {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	wsID, agentID, taskID := idgen.New(), idgen.New(), idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "squad-session"); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: agentID, WorkspaceID: wsID, Name: "a", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: "m", WorkDir: "/tmp/work", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: taskID, WorkspaceID: wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeSquad, Status: domain.TaskExecuting,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id = ?`, taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})
	return db, wsID, agentID, taskID
}

func TestSquadReuse(t *testing.T) {
	db, _, agentID, taskID := setupSquadSession(t)
	ctx := context.Background()
	svc := &session.Service{DB: db}

	first, reused, err := svc.ReuseOrCreate(ctx, taskID, agentID)
	if err != nil || reused || first.Ordinal != 1 {
		t.Fatalf("首次创建 = %+v, reused=%v, err=%v", first, reused, err)
	}
	if first.ResumeFingerprint == "" {
		t.Fatal("应记录恢复指纹")
	}

	// 记录 Provider 会话后，同配置可复用
	if err := mysqlstore.SetSessionACPID(ctx, db, first.ID, "acp-1", first.ResumeFingerprint); err != nil {
		t.Fatal(err)
	}
	again, reused, err := svc.ReuseOrCreate(ctx, taskID, agentID)
	if err != nil || !reused || again.ID != first.ID || again.ACPSessionID != "acp-1" {
		t.Fatalf("复用 = %+v, reused=%v, err=%v", again, reused, err)
	}

	// 修改 Agent 语义 → 指纹不匹配，新建 ordinal 2
	if _, err := db.Exec(`UPDATE agents SET system_prompt = 'new prompt' WHERE id = ?`, agentID); err != nil {
		t.Fatal(err)
	}
	third, reused, err := svc.ReuseOrCreate(ctx, taskID, agentID)
	if err != nil || reused || third.Ordinal != 2 || third.ID == first.ID {
		t.Fatalf("指纹变化后 = %+v, reused=%v, err=%v", third, reused, err)
	}
}

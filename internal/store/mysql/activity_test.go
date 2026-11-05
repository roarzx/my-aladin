//go:build integration

package mysql_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestActivityAppendAndList(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBRaw(t)

	wsID, taskID := idgen.New(), idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "activity"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(ctx, db, domain.Task{
		ID: taskID, WorkspaceID: wsID, CreatedBy: 1, Name: "t",
		Mode: domain.ModeOrdinary, Status: domain.TaskExecuting,
		ConfigSnapshot: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})

	first, err := mysqlstore.AppendActivity(ctx, db, taskID, "task.created", map[string]any{"name": "t"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := mysqlstore.AppendActivity(ctx, db, taskID, "step.dispatched", map[string]any{"step_id": "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq = %d/%d", first.Seq, second.Seq)
	}

	all, err := mysqlstore.ListActivities(ctx, db, taskID, 0, 100)
	if err != nil || len(all) != 2 {
		t.Fatalf("事件 = %d, err=%v", len(all), err)
	}
	after, err := mysqlstore.ListActivities(ctx, db, taskID, first.Seq, 100)
	if err != nil || len(after) != 1 || after[0].EventID != second.EventID {
		t.Fatalf("游标语义错误: %+v, err=%v", after, err)
	}
	if !strings.Contains(string(second.Payload), `"step_id":"s1"`) {
		t.Fatalf("payload = %s", second.Payload)
	}
}

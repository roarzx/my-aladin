//go:build integration

package orchestration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type recordRunner struct{ ids []string }

func (r *recordRunner) RunOnce(_ context.Context, taskID string) error {
	r.ids = append(r.ids, taskID)
	return nil
}

func insertTaskWithMode(t *testing.T, db *sql.DB, wsID string, mode domain.TaskMode) string {
	t.Helper()
	id := idgen.New()
	now := time.Now().UTC()
	if err := mysqlstore.InsertTask(context.Background(), db, domain.Task{
		ID: id, WorkspaceID: wsID, CreatedBy: 1, Name: "t", Mode: mode,
		Status: domain.TaskExecuting, ConfigSnapshot: json.RawMessage(`{}`),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCompositeRunner(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	wsID := idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "composite"); err != nil {
		t.Fatal(err)
	}
	ordinary := insertTaskWithMode(t, db, wsID, domain.ModeOrdinary)
	squadTask := insertTaskWithMode(t, db, wsID, domain.ModeSquad)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})

	planRunner := &recordRunner{}
	squadRunner := &recordRunner{}
	composite := &orchestration.CompositeRunner{DB: db, Plan: planRunner, Squad: squadRunner}

	if err := composite.RunOnce(ctx, ordinary); err != nil {
		t.Fatal(err)
	}
	if err := composite.RunOnce(ctx, squadTask); err != nil {
		t.Fatal(err)
	}
	if len(planRunner.ids) != 1 || planRunner.ids[0] != ordinary {
		t.Fatalf("PlanRunner 调用 = %+v", planRunner.ids)
	}
	if len(squadRunner.ids) != 1 || squadRunner.ids[0] != squadTask {
		t.Fatalf("SquadRunner 调用 = %+v", squadRunner.ids)
	}
}

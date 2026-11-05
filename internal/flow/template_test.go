//go:build integration

package flow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/roarzx/my-aladin/internal/flow"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

func setupFlow(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := testkit.NewDBRaw(t)
	wsID := idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "flow"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM flow_node_runs WHERE run_id IN (SELECT id FROM flow_runs WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?))`, wsID)
		_, _ = db.Exec(`DELETE FROM flow_runs WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM flow_templates WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})
	return db, wsID
}

func TestPublish(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()

	v1, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil || v1.Version != 1 {
		t.Fatalf("发布 v1: %+v, err=%v", v1, err)
	}
	v2, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 1)
	if err != nil || v2.Version != 2 || v2.ID == v1.ID {
		t.Fatalf("发布 v2: %+v, err=%v", v2, err)
	}
	// expected_version 不匹配 → 冲突
	if _, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 1); !hasCode(err, errcode.RevisionConflict) {
		t.Fatalf("期望 REVISION_CONFLICT，实际: %v", err)
	}
	// 节点顺序不可配置
	bad := []flow.NodeSpec{{Key: flow.NodeMarsAcceptance}, {Key: flow.NodeEnvironmentPrepare}, {Key: flow.NodeAgentPrompt}}
	if _, err := flow.Publish(ctx, db, wsID, "验收2", bad, 0); !hasCode(err, errcode.ValidationFailed) {
		t.Fatalf("非法节点顺序应拒绝，实际: %v", err)
	}

	// 未停用不能删除
	if err := flow.Delete(ctx, db, v1.ID); !hasCode(err, errcode.Conflict) {
		t.Fatalf("启用中删除应拒绝，实际: %v", err)
	}
	if err := flow.Disable(ctx, db, v1.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := flow.Delete(ctx, db, v1.ID); err != nil {
		t.Fatalf("停用后删除: %v", err)
	}
}

func TestRunSnapshot(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, runTaskCleanup := createFlowTask(t, db, wsID)
	defer runTaskCleanup()

	run, nodes, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{"branch":"main"}`))
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if len(nodes) != 3 || nodes[0].NodeKey != string(flow.NodeEnvironmentPrepare) ||
		nodes[1].NodeKey != string(flow.NodeAgentPrompt) ||
		nodes[2].NodeKey != string(flow.NodeMarsAcceptance) {
		t.Fatalf("节点 = %+v", nodes)
	}

	// 模板后续发布新版本不影响已创建 Run
	if _, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 1); err != nil {
		t.Fatal(err)
	}
	got, gotNodes, err := flow.GetRun(ctx, db, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TemplateVersion != 1 || len(gotNodes) != 3 {
		t.Fatalf("运行快照 = %+v, nodes=%d", got, len(gotNodes))
	}
	var snapshot flow.Template
	if err := json.Unmarshal(got.TemplateSnapshot, &snapshot); err != nil {
		t.Fatalf("快照解析: %v", err)
	}
	if snapshot.Version != 1 || snapshot.Name != "验收" {
		t.Fatalf("快照内容 = %+v", snapshot)
	}
}

func createFlowTask(t *testing.T, db *sql.DB, wsID string) (string, func()) {
	t.Helper()
	taskID := idgen.New()
	_, err := db.Exec(`
		INSERT INTO tasks (id, workspace_id, created_by, name, mode, status,
			version, activity_seq, config_snapshot, created_at, updated_at)
		VALUES (?, ?, 1, 'flow', 'ordinary', 'executing', 0, 0, '{}', UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))`,
		taskID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	return taskID, func() {
		_, _ = db.Exec(`DELETE FROM flow_node_runs WHERE run_id IN (SELECT id FROM flow_runs WHERE task_id = ?)`, taskID)
		_, _ = db.Exec(`DELETE FROM flow_runs WHERE task_id = ?`, taskID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, taskID)
	}
}

func hasCode(err error, code errcode.Code) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

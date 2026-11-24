//go:build e2e

// D-1 回归：任务必须真正拿到计划分支，否则 daemon 会跳过 push 与 PR。
package e2e

import (
	"context"
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
)

// setupIssueWithRepo 在标准夹具上追加一个仓库，并返回仓库 ID。
func setupIssueWithRepo(t *testing.T) (*e2eEnv, string) {
	t.Helper()
	env := setupIssue(t)
	repoID := idgen.New()
	ctx := context.Background()
	if err := mysqlstore.CreateRepository(ctx, env.db, mysqlstore.Repository{
		ID: repoID, WorkspaceID: env.wsID,
		URL: "https://example.com/demo.git", DefaultBranch: "main", Provider: "git",
	}); err != nil {
		t.Fatalf("创建仓库: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.db.Exec(`DELETE FROM task_repositories WHERE repository_id = ?`, repoID)
		_, _ = env.db.Exec(`DELETE FROM repositories WHERE id = ?`, repoID)
	})
	return env, repoID
}

func (e *e2eEnv) createIssueWithRepo(t *testing.T, repoID string) string {
	t.Helper()
	created, err := (&task.Service{DB: e.db}).Create(context.Background(), 1, e.wsID, task.CreateRequest{
		Name: "带仓库的 Issue", Requirement: "改点东西", Mode: domain.ModeOrdinary,
		AgentIDs: []string{e.agentID}, PlannerAgentID: e.agentID,
		Repositories: []string{repoID},
	})
	if err != nil {
		t.Fatalf("创建 Issue: %v", err)
	}
	return created.ID
}

func TestIssueTurnAssignsPlanBranch(t *testing.T) {
	env, repoID := setupIssueWithRepo(t)
	ctx := context.Background()
	taskID := env.createIssueWithRepo(t, repoID)
	want := "aladdin/" + taskID[:8]

	// 创建时还没有分支。
	created, err := mysqlstore.GetTask(ctx, env.db, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if created.PlanBranch != "" {
		t.Fatalf("创建时 plan_branch = %q，期望空", created.PlanBranch)
	}

	// 第一轮开始：分配分支并随派发下发。
	if _, err := env.service.StartTurn(ctx, taskID, "开始"); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if got := env.runtime.last().Branch; got != want {
		t.Fatalf("派发 branch = %q，期望 %q", got, want)
	}

	got, err := mysqlstore.GetTask(ctx, env.db, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanBranch != want {
		t.Fatalf("tasks.plan_branch = %q，期望 %q", got.PlanBranch, want)
	}
	var target string
	if err := env.db.QueryRowContext(ctx,
		`SELECT target_branch FROM task_repositories WHERE task_id = ?`, taskID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != want {
		t.Fatalf("task_repositories.target_branch = %q，期望 %q", target, want)
	}

	// 第二轮（用户追问）复用同一分支，不新建。
	env.event(t, "turn_done", env.runtime.last().ExecutionID, map[string]any{"reply": "先这样"})
	if _, err := env.service.Message(ctx, taskID, "再改一点"); err != nil {
		t.Fatalf("Message: %v", err)
	}
	if got := env.runtime.last().Branch; got != want {
		t.Fatalf("追问 branch = %q，期望复用 %q", got, want)
	}
}

func TestIssueWithoutRepoHasNoBranch(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "无仓库任务")

	if _, err := env.service.StartTurn(ctx, taskID, "开始"); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if got := env.runtime.last().Branch; got != "" {
		t.Fatalf("无仓库时不应分配分支，实际 %q", got)
	}
	got, err := mysqlstore.GetTask(ctx, env.db, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanBranch != "" {
		t.Fatalf("tasks.plan_branch = %q，期望空", got.PlanBranch)
	}
}

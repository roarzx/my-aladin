//go:build integration

package plan_test

import (
	"context"
	"strings"
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/plan"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// bindRepository 给任务绑定一个仓库，target_branch 初始指向默认分支。
func bindRepository(t *testing.T, f *fixture, defaultBranch string) string {
	t.Helper()
	ctx := context.Background()
	repoID := idgen.New()
	mustExec(t, f.db,
		`INSERT INTO repositories (id, workspace_id, url, default_branch, provider) VALUES (?, ?, ?, ?, ?)`,
		repoID, f.wsID, "https://example.com/repo.git", defaultBranch, "github")
	if err := mysqlstore.InsertTaskRepository(ctx, f.db, f.taskID, repoID,
		strings.Repeat("b", 40), defaultBranch); err != nil {
		t.Fatalf("绑定任务仓库: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.db.Exec(`DELETE FROM task_repositories WHERE task_id = ?`, f.taskID)
		_, _ = f.db.Exec(`DELETE FROM repositories WHERE id = ?`, repoID)
	})
	return repoID
}

// startTask 走完提计划 → 确认 → 启动的完整流程。
func startTask(t *testing.T, f *fixture) (domain.Task, *plan.Service) {
	t.Helper()
	ctx := context.Background()
	svc := &plan.Service{DB: f.db}

	p, _, err := svc.Propose(ctx, f.taskID, f.proposal(1))
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := svc.Confirm(ctx, p.ID, p.Revision, p.ContentHash, 1); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	task, err := svc.Start(ctx, f.taskID, 0)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return task, svc
}

func TestStartWritesPlanBranchToTaskRepositories(t *testing.T) {
	f := setup(t)
	repoID := bindRepository(t, f, "trunk")

	task, _ := startTask(t, f)
	if !strings.HasPrefix(task.PlanBranch, "aladdin/") {
		t.Fatalf("计划分支 = %q", task.PlanBranch)
	}
	if task.Status != domain.TaskExecuting {
		t.Fatalf("任务状态 = %q", task.Status)
	}

	repos, err := mysqlstore.ListTaskRepositories(context.Background(), f.db, f.taskID)
	if err != nil {
		t.Fatalf("读取任务仓库: %v", err)
	}
	if len(repos) != 1 || repos[0].RepositoryID != repoID {
		t.Fatalf("任务仓库 = %+v", repos)
	}
	// D6：启动后目标分支应为计划分支，默认分支保持不变。
	if repos[0].TargetBranch != task.PlanBranch {
		t.Fatalf("目标分支 = %q，期望 %q", repos[0].TargetBranch, task.PlanBranch)
	}
	if repos[0].DefaultBranch != "trunk" {
		t.Fatalf("默认分支 = %q，不应被改写", repos[0].DefaultBranch)
	}
}

func TestStartKeepsExistingPlanBranch(t *testing.T) {
	f := setup(t)
	bindRepository(t, f, "main")
	// 任务已带计划分支（例如恢复后重试启动），启动时不应重新生成。
	mustExec(t, f.db, `UPDATE tasks SET plan_branch = ? WHERE id = ?`, "aladdin/fixed", f.taskID)

	task, _ := startTask(t, f)
	if task.PlanBranch != "aladdin/fixed" {
		t.Fatalf("计划分支 = %q", task.PlanBranch)
	}

	repos, err := mysqlstore.ListTaskRepositories(context.Background(), f.db, f.taskID)
	if err != nil {
		t.Fatalf("读取任务仓库: %v", err)
	}
	if len(repos) != 1 || repos[0].TargetBranch != "aladdin/fixed" {
		t.Fatalf("任务仓库 = %+v", repos)
	}
}

package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitIn(t *testing.T, dir, message string) {
	t.Helper()
	gitCmd(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", "-A")
	gitCmd(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-m", message)
}

// setupOrigin 创建一个本地 bare 仓库，并推入初始提交。
func setupOrigin(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitCmd(t, root, "init", "--bare", origin)

	seed := filepath.Join(root, "seed")
	gitCmd(t, root, "clone", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("init\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitIn(t, seed, "init")
	gitCmd(t, seed, "push", "origin", "HEAD:main")
	// 不同平台 init 的默认分支不同（main/master）；显式设置远端 HEAD，
	// 否则 Linux 上 clone 会得到空仓库（HEAD 指向不存在的分支）。
	gitCmd(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	return origin
}

func TestPrepareCloneAndBranch(t *testing.T) {
	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")

	w, err := Prepare(context.Background(), RepoSpec{URL: origin, Dir: dir, PlanBranch: "feature"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if w.PlanBranch != "feature" {
		t.Fatalf("PlanBranch = %q", w.PlanBranch)
	}
	if branch := gitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "feature" {
		t.Fatalf("当前分支 = %q", branch)
	}
}

func TestPrepareRejectsHeadMismatch(t *testing.T) {
	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")

	_, err := Prepare(context.Background(), RepoSpec{
		URL: origin, Dir: dir, PlanBranch: "feature",
		BaseSHA: "0000000000000000000000000000000000000000",
	})
	if !errors.Is(err, ErrHeadMismatch) {
		t.Fatalf("期望 ErrHeadMismatch，实际: %v", err)
	}
}

func TestDirtyWorkspaceIsNotCleaned(t *testing.T) {
	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")
	ctx := context.Background()

	w, err := Prepare(ctx, RepoSpec{URL: origin, Dir: dir, PlanBranch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	dirtyFile := filepath.Join(w.Dir, "local.txt")
	if err := os.WriteFile(dirtyFile, []byte("user change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.EnsureClean(ctx); !errors.Is(err, ErrDirtyWorkspace) {
		t.Fatalf("期望 ErrDirtyWorkspace，实际: %v", err)
	}

	// 再次 Prepare 不能清理用户改动
	if _, err := Prepare(ctx, RepoSpec{URL: origin, Dir: w.Dir, PlanBranch: "feature"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dirtyFile); err != nil {
		t.Fatal("脏工作区被清理")
	}
}

func TestPushOnlyPlanBranch(t *testing.T) {
	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")
	ctx := context.Background()

	w, err := Prepare(ctx, RepoSpec{URL: origin, Dir: dir, PlanBranch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitIn(t, dir, "change")

	if err := w.Push(ctx, "main"); !errors.Is(err, ErrBranchNotAllowed) {
		t.Fatalf("非计划分支应被拒绝，实际: %v", err)
	}
	if err := w.Push(ctx, "feature"); err != nil {
		t.Fatalf("推送计划分支: %v", err)
	}
	gitCmd(t, origin, "rev-parse", "--verify", "refs/heads/feature")
}

func TestCommitAllSetsBotIdentityAndPushes(t *testing.T) {
	// 隔离本机 git 配置，确保提交使用仓库本地身份。
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")
	ctx := context.Background()

	w, err := Prepare(ctx, RepoSpec{URL: origin, Dir: dir, PlanBranch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := w.HasChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("干净工作区不应有改动")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err = w.HasChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("新增文件应判定为有改动")
	}

	sha, err := w.CommitAll(ctx, "aladdin: task")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("commit_sha = %q", sha)
	}
	if head := gitCmd(t, dir, "rev-parse", "HEAD"); head != sha {
		t.Fatalf("HEAD = %q, commit_sha = %q", head, sha)
	}
	if author := gitCmd(t, dir, "log", "-1", "--format=%an <%ae>"); author != "Aladdin Bot <aladdin@example.com>" {
		t.Fatalf("提交身份 = %q", author)
	}
	if subject := gitCmd(t, dir, "log", "-1", "--format=%s"); subject != "aladdin: task" {
		t.Fatalf("提交信息 = %q", subject)
	}
	if changed, err := w.HasChanges(ctx); err != nil || changed {
		t.Fatalf("提交后应为干净工作区: changed=%v err=%v", changed, err)
	}

	if err := w.Push(ctx, "feature"); err != nil {
		t.Fatalf("推送计划分支: %v", err)
	}
	if remote := gitCmd(t, origin, "rev-parse", "refs/heads/feature"); remote != sha {
		t.Fatalf("远端分支 = %q, 期望 %q", remote, sha)
	}
}

func TestDiffAndCommits(t *testing.T) {
	origin := setupOrigin(t)
	dir := filepath.Join(t.TempDir(), "work")
	ctx := context.Background()

	w, err := Prepare(ctx, RepoSpec{URL: origin, Dir: dir, PlanBranch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitIn(t, dir, "change")

	diff, err := w.Diff(ctx, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "a.txt") {
		t.Fatalf("diff = %q", diff)
	}

	commits, err := w.Commits(ctx, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || !strings.Contains(commits[0], "change") {
		t.Fatalf("commits = %+v", commits)
	}
}

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 自动提交使用的本地 Git 身份；仅在仓库未配置时写入。
const (
	botName  = "Aladdin Bot"
	botEmail = "aladdin@example.com"
)

var (
	// ErrDirtyWorkspace 表示工作区有未提交改动；调用方不得自动清理。
	ErrDirtyWorkspace = errors.New("workspace: 工作区有未提交改动")
	// ErrBranchNotAllowed 表示只允许推送任务计划分支。
	ErrBranchNotAllowed = errors.New("workspace: 只允许推送计划分支")
	// ErrHeadMismatch 表示 HEAD 与基线 SHA 不一致。
	ErrHeadMismatch = errors.New("workspace: HEAD 与基线不一致")
)

// RepoSpec 描述任务绑定的仓库与分支。
type RepoSpec struct {
	URL        string
	Dir        string
	PlanBranch string
	BaseSHA    string
}

// Workspace 是一个已准备好的工作区。
type Workspace struct {
	Dir        string
	PlanBranch string
}

// Prepare 克隆或打开工作区、切换/创建计划分支，并校验基线。
//
// 不清理脏工作区：有未提交改动时由调用方通过 EnsureClean 判断并进入 blocked。
func Prepare(ctx context.Context, spec RepoSpec) (*Workspace, error) {
	if spec.Dir == "" {
		return nil, errors.New("workspace: Dir 不能为空")
	}
	if _, err := os.Stat(spec.Dir); errors.Is(err, os.ErrNotExist) {
		if spec.URL == "" {
			return nil, errors.New("workspace: 目录不存在且未提供 URL")
		}
		// git clone 不会创建多层父目录（如 <work_dir>/aladdin）。
		if err := os.MkdirAll(filepath.Dir(spec.Dir), 0o755); err != nil {
			return nil, fmt.Errorf("创建父目录: %w", err)
		}
		if out, err := runGit(ctx, "", "clone", spec.URL, spec.Dir); err != nil {
			return nil, fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(out))
		}
	} else if err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(spec.Dir, ".git")); err != nil {
		return nil, fmt.Errorf("workspace: %s 已存在但不是 git 仓库: %w", spec.Dir, err)
	}

	w := &Workspace{Dir: spec.Dir, PlanBranch: spec.PlanBranch}
	if spec.PlanBranch != "" {
		switch {
		case refExists(ctx, w.Dir, "refs/heads/"+spec.PlanBranch):
			if out, err := runGit(ctx, w.Dir, "checkout", spec.PlanBranch); err != nil {
				return nil, fmt.Errorf("切换分支: %w: %s", err, strings.TrimSpace(out))
			}
		case refExists(ctx, w.Dir, "refs/remotes/origin/"+spec.PlanBranch):
			// 远端已有计划分支（换机/重试）：基于远端恢复，避免 non-fast-forward
			if out, err := runGit(ctx, w.Dir, "checkout", "-B", spec.PlanBranch,
				"origin/"+spec.PlanBranch); err != nil {
				return nil, fmt.Errorf("恢复远端分支: %w: %s", err, strings.TrimSpace(out))
			}
		default:
			if out, err := runGit(ctx, w.Dir, "checkout", "-b", spec.PlanBranch); err != nil {
				return nil, fmt.Errorf("创建分支: %w: %s", err, strings.TrimSpace(out))
			}
		}
	}

	if spec.BaseSHA != "" {
		head, err := w.Head(ctx)
		if err != nil {
			return nil, err
		}
		if head != spec.BaseSHA {
			return nil, fmt.Errorf("%w: HEAD=%s 基线=%s", ErrHeadMismatch, head, spec.BaseSHA)
		}
	}
	return w, nil
}

// Head 返回当前 HEAD。
func (w *Workspace) Head(ctx context.Context) (string, error) {
	out, err := runGit(ctx, w.Dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// Status 返回 porcelain 状态。
func (w *Workspace) Status(ctx context.Context) (string, error) {
	return runGit(ctx, w.Dir, "status", "--porcelain")
}

// EnsureClean 在存在未提交改动时返回 ErrDirtyWorkspace。
func (w *Workspace) EnsureClean(ctx context.Context) error {
	out, err := w.Status(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("%w: %s", ErrDirtyWorkspace, firstLine(out))
	}
	return nil
}

// HasChanges 报告工作区是否有未提交改动（含未跟踪文件）。
func (w *Workspace) HasChanges(ctx context.Context) (bool, error) {
	out, err := w.Status(ctx)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll 暂存全部改动并提交，返回新的 HEAD。
//
// 提交前若仓库未配置 user.name/user.email，则只在本仓库设置 Aladdin Bot 身份。
func (w *Workspace) CommitAll(ctx context.Context, message string) (string, error) {
	if err := w.ensureIdentity(ctx); err != nil {
		return "", err
	}
	if out, err := runGit(ctx, w.Dir, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runGit(ctx, w.Dir, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(out))
	}
	return w.Head(ctx)
}

func (w *Workspace) ensureIdentity(ctx context.Context) error {
	if err := ensureGitConfig(ctx, w.Dir, "user.name", botName); err != nil {
		return err
	}
	return ensureGitConfig(ctx, w.Dir, "user.email", botEmail)
}

// ensureGitConfig 仅在键无值时写入（--get 未命中时 git 以 1 退出）。
func ensureGitConfig(ctx context.Context, dir, key, value string) error {
	if out, err := runGit(ctx, dir, "config", "--get", key); err == nil && strings.TrimSpace(out) != "" {
		return nil
	}
	if out, err := runGit(ctx, dir, "config", key, value); err != nil {
		return fmt.Errorf("git config %s: %w: %s", key, err, strings.TrimSpace(out))
	}
	return nil
}

// Diff 返回区间内改动的统计（revisionRange 为空表示工作区）。
func (w *Workspace) Diff(ctx context.Context, revisionRange string) (string, error) {
	args := []string{"diff", "--stat"}
	if revisionRange != "" {
		args = append(args, revisionRange)
	}
	return runGit(ctx, w.Dir, args...)
}

// Commits 返回区间内的提交（sha + subject）。
func (w *Workspace) Commits(ctx context.Context, revisionRange string) ([]string, error) {
	args := []string{"log", "--format=%H%x09%s"}
	if revisionRange != "" {
		args = append(args, revisionRange)
	}
	out, err := runGit(ctx, w.Dir, args...)
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			commits = append(commits, line)
		}
	}
	return commits, nil
}

// Push 只允许推送任务计划分支。
func (w *Workspace) Push(ctx context.Context, branch string) error {
	if w.PlanBranch == "" || branch != w.PlanBranch {
		return fmt.Errorf("%w: %q（计划分支 %q）", ErrBranchNotAllowed, branch, w.PlanBranch)
	}
	out, err := runGit(ctx, w.Dir, "push", "origin", branch)
	if err != nil {
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func refExists(ctx context.Context, dir, ref string) bool {
	_, err := runGit(ctx, dir, "rev-parse", "--verify", ref)
	return err == nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

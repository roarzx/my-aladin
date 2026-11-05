package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		if out, err := runGit(ctx, "", "clone", spec.URL, spec.Dir); err != nil {
			return nil, fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(out))
		}
	} else if err != nil {
		return nil, err
	}

	w := &Workspace{Dir: spec.Dir, PlanBranch: spec.PlanBranch}
	if spec.PlanBranch != "" {
		if _, err := runGit(ctx, w.Dir, "rev-parse", "--verify", "refs/heads/"+spec.PlanBranch); err != nil {
			if out, err := runGit(ctx, w.Dir, "checkout", "-b", spec.PlanBranch); err != nil {
				return nil, fmt.Errorf("创建分支: %w: %s", err, strings.TrimSpace(out))
			}
		} else if out, err := runGit(ctx, w.Dir, "checkout", spec.PlanBranch); err != nil {
			return nil, fmt.Errorf("切换分支: %w: %s", err, strings.TrimSpace(out))
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

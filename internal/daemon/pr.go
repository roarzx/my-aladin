package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// PRRequest 描述一次 PR 创建/查询所需的字段。
type PRRequest struct {
	RepoURL string
	Base    string
	Head    string
	Title   string
	Body    string
}

// PRCreator 创建 PR（或查询已存在 PR）并返回可访问的 URL。
//
// 抽成接口是为了测试注入：单测不调用真实 gh / GitHub。
type PRCreator interface {
	EnsurePR(ctx context.Context, dir string, req PRRequest) (string, error)
}

// GHCreator 使用本机 gh CLI 创建 PR；分支已存在 PR 时查询其 URL。
//
// 认证依赖机器上的 gh auth setup-git（部署要求）。
type GHCreator struct{}

// EnsurePR 实现 PRCreator。
func (GHCreator) EnsurePR(ctx context.Context, dir string, req PRRequest) (string, error) {
	repo, err := repoSlug(req.RepoURL)
	if err != nil {
		return "", err
	}
	base := req.Base
	if base == "" {
		base = "main"
	}

	args := []string{
		"pr", "create",
		"--repo", repo,
		"--base", base,
		"--head", req.Head,
		"--title", req.Title,
	}
	if req.Body != "" {
		args = append(args, "--body", req.Body)
	}
	stdout, stderr, createErr := runCLI(ctx, dir, "gh", args...)
	if createErr == nil {
		if prURL := lastNonEmptyLine(stdout); prURL != "" {
			return prURL, nil
		}
	}

	// create 失败常见原因是 PR 已存在；此时按分支查询 URL。
	viewOut, viewErr, viewRunErr := runCLI(ctx, dir, "gh",
		"pr", "view", req.Head, "--repo", repo, "--json", "url")
	if viewRunErr == nil {
		var view struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(viewOut), &view) == nil && view.URL != "" {
			return view.URL, nil
		}
	}

	if createErr != nil {
		return "", fmt.Errorf("gh pr create: %w: %s", createErr, strings.TrimSpace(stderr))
	}
	if viewRunErr != nil {
		return "", fmt.Errorf("gh pr view: %w: %s", viewRunErr, strings.TrimSpace(viewErr))
	}
	return "", fmt.Errorf("gh pr view 未返回 PR URL: %s", strings.TrimSpace(viewOut))
}

// repoSlug 从 https/ssh 形式的仓库地址解析 owner/repo。
func repoSlug(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("repo_url 为空")
	}
	// git@github.com:owner/repo.git
	if strings.HasPrefix(s, "git@") {
		if i := strings.IndexByte(s, ':'); i >= 0 {
			s = s[i+1:]
		}
	} else if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = strings.TrimPrefix(u.Path, "/")
	}
	s = strings.TrimSuffix(strings.Trim(s, "/"), ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", fmt.Errorf("无法从 %q 解析 owner/repo", raw)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

func runCLI(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return string(out), stderr.String(), err
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

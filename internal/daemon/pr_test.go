package daemon

import "testing"

func TestRepoSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"git@github.com:acme/repo.git", "acme/repo"},
		{"https://github.com/acme/repo.git", "acme/repo"},
		{"https://github.com/acme/repo", "acme/repo"},
		{"https://gitlab.example.com/group/sub/repo.git", "sub/repo"},
	}
	for _, c := range cases {
		got, err := repoSlug(c.in)
		if err != nil {
			t.Fatalf("repoSlug(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("repoSlug(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "repo-only", "git@github.com:repo.git"} {
		if _, err := repoSlug(bad); err == nil {
			t.Fatalf("repoSlug(%q) 应报错", bad)
		}
	}
}

func TestLastNonEmptyLine(t *testing.T) {
	if got := lastNonEmptyLine("https://example.com/pull/1\n\n"); got != "https://example.com/pull/1" {
		t.Fatalf("lastNonEmptyLine = %q", got)
	}
	if got := lastNonEmptyLine("  \n"); got != "" {
		t.Fatalf("空输出应返回空串: %q", got)
	}
}

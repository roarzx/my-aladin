package stats

import (
	"strings"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("空样本 = %d，期望 0", got)
	}
	samples := []int64{100, 200, 300, 400, 500}
	if got := percentile(samples, 50); got != 300 {
		t.Fatalf("p50 = %d，期望 300", got)
	}
	// 最近秩法：5 个样本的 p95 落在最后一个。
	if got := percentile(samples, 95); got != 500 {
		t.Fatalf("p95 = %d，期望 500", got)
	}
	if got := percentile(samples, 1); got != 100 {
		t.Fatalf("p1 = %d，期望 100", got)
	}
}

func TestTopN(t *testing.T) {
	counts := map[string]int{"ls": 5, "git": 3, "cd": 2, "cat": 1}
	got := topN(counts, 2, "其他")
	if len(got) != 3 {
		t.Fatalf("topN = %+v", got)
	}
	if got[0].Name != "ls" || got[0].Count != 5 || got[1].Name != "git" {
		t.Fatalf("排序错误: %+v", got)
	}
	if got[2].Name != "其他" || got[2].Count != 3 {
		t.Fatalf("其余合计错误: %+v", got[2])
	}
	// 不设汇总名时直接截断。
	if got := topN(counts, 2, ""); len(got) != 2 {
		t.Fatalf("截断 = %+v", got)
	}
}

func TestToolName(t *testing.T) {
	cases := []struct{ kind, title, want string }{
		{"edit", "", "edit"},
		{"", "ls -la /tmp/x", "ls"},
		{"", "  ", "unknown"},
		{"", "", "unknown"},
		{"", strings.Repeat("a", 40), strings.Repeat("a", 24)},
	}
	for _, tc := range cases {
		if got := ToolName(tc.kind, tc.title); got != tc.want {
			t.Fatalf("ToolName(%q, %q) = %q，期望 %q", tc.kind, tc.title, got, tc.want)
		}
	}
}

func TestRenderPrometheus(t *testing.T) {
	overview := Overview{
		Days:        7,
		GeneratedAt: time.Unix(1700000000, 0).UTC(),
		Issues: IssueStats{
			Total:    3,
			ByStatus: map[string]int{"executing": 1, "completed": 2},
		},
		Runs: RunStats{
			Total:       2,
			ByStatus:    map[string]int{"completed": 1, "failed": 1},
			Finished:    2,
			Succeeded:   1,
			SuccessRate: 0.5,
			ErrorRate:   0.5,
			Durations:   map[string][]int64{"completed": {61000}, "failed": {30000}},
			P50Ms:       30000,
			P95Ms:       61000,
		},
		Failures:   []NamedCount{{Name: "network", Count: 1}},
		Tools:      []ToolStat{{Name: "ls", Count: 3, Failed: 1, P50Ms: 120, P95Ms: 400}},
		ToolsByDay: []DayCount{{Date: "2026-11-24", Count: 3}},
		Tokens: TokenStats{
			Input:   1000,
			Output:  200,
			ByModel: []ModelTokens{{Model: "claude-sonnet-5-5", Input: 1000, Output: 200}},
			Daily:   []DayTokens{{Date: "2026-11-24", Input: 1000, Output: 200}},
		},
		Machines:     []NamedCount{{Name: "online", Count: 1}},
		Availability: 0.5,
		Agents: []AgentStats{{
			AgentID: "agent-123456", Name: "Claude", RuntimeType: "claude_code",
			Runs: 2, Finished: 2, Succeeded: 1, Failed: 1,
			InputTokens: 900, OutputTokens: 90,
		}},
	}
	text := RenderPrometheus(overview)

	wants := []string{
		`aladdin_issues{status="completed"} 2`,
		`aladdin_runs_total{status="failed"} 1`,
		`aladdin_run_success_ratio 0.5000`,
		`aladdin_run_duration_seconds_bucket{status="completed",le="60"} 0`,
		`aladdin_run_duration_seconds_bucket{status="completed",le="120"} 1`,
		`aladdin_run_duration_seconds_count{status="completed"} 1`,
		`aladdin_run_failures_total{error_class="network"} 1`,
		`aladdin_tool_calls_total{tool="ls",status="ok"} 2`,
		`aladdin_tool_calls_total{tool="ls",status="failed"} 1`,
		`aladdin_tokens_total{direction="input",model="claude-sonnet-5-5"} 1000`,
		`aladdin_machines{status="online"} 1`,
		`aladdin_machines_availability 0.5000`,
		`aladdin_run_error_ratio 0.5000`,
		`aladdin_tool_duration_seconds{tool="ls",quantile="0.5"} 0.12`,
		`aladdin_agent_runs_total{agent="Claude/agent-12",runtime="claude_code",status="completed"} 1`,
		`aladdin_agent_tokens_total{agent="Claude/agent-12",direction="input"} 900`,
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少指标行 %q\n---\n%s", want, text)
		}
	}
	// 高基数字段不能进指标标签。
	for _, forbidden := range []string{"task_id", "run_id", "execution_id"} {
		if strings.Contains(text, forbidden+"=") {
			t.Fatalf("指标里出现了高基数标签 %q", forbidden)
		}
	}
}

func TestEscapeLabel(t *testing.T) {
	got := escapeLabel("a\"b\\c\nd")
	want := `a\"b\\c\nd`
	if got != want {
		t.Fatalf("escapeLabel = %q，期望 %q", got, want)
	}
}

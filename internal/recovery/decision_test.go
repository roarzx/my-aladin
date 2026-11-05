package recovery

import "testing"

func TestDecideTable(t *testing.T) {
	cases := []struct {
		name     string
		evidence Evidence
		want     Verdict
	}{
		{
			name: "有新提交 + 摘要存在 + digest 一致 → 已完成",
			evidence: Evidence{GitEvidenceKnown: true, NewCommits: true,
				SummaryPresent: true, SummaryDigestMatches: true},
			want: VerdictCompleted,
		},
		{
			name:     "无新提交 + 无摘要 → 未执行",
			evidence: Evidence{GitEvidenceKnown: true},
			want:     VerdictFailed,
		},
		{
			name:     "有新提交 + 无摘要 → 部分完成，转人工",
			evidence: Evidence{GitEvidenceKnown: true, NewCommits: true},
			want:     VerdictUndecidable,
		},
		{
			name:     "机器在线且 execution 仍在 running → 不要抢",
			evidence: Evidence{MachineOnline: true, ExecutionRunning: true, GitEvidenceKnown: true, NewCommits: true},
			want:     VerdictUndecidable,
		},
		{
			name:     "证据不足（机器离线/无 git 证据）→ 未知",
			evidence: Evidence{},
			want:     VerdictUndecidable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.evidence); got != tc.want {
				t.Fatalf("Decide = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

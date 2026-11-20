package acp

import "testing"

func TestMergeEnvOverridesAndDedupes(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "PATH=/usr/bin"}
	extra := []string{"PATH=/opt/aladdin:/usr/bin", "ALADDIN_TOKEN=tok"}

	got := mergeEnv(base, extra)
	want := []string{"PATH=/opt/aladdin:/usr/bin", "HOME=/root", "ALADDIN_TOKEN=tok"}
	if len(got) != len(want) {
		t.Fatalf("merged = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged = %v，期望 %v", got, want)
		}
	}

	// 只覆盖第一个同名键，不应出现重复 PATH。
	seen := map[string]int{}
	for _, kv := range got {
		seen[envKey(kv)]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Fatalf("%s 重复 %d 次: %v", key, count, got)
		}
	}
}

package agentctx

import (
	"errors"
	"strings"
	"testing"
)

func budgetBundle() Bundle {
	return Assemble(Request{
		Constraints:  []string{"不得修改数据库结构"},
		Requirements: []string{"接口必须幂等"},
		Decisions:    []Decision{{Text: "使用 Redis 锁"}},
		StepInput:    "实现 X",
		Recent:       []string{"最近 1", "最近 2", "最近 3", "最近 4", "最近 5"},
		History:      []string{"历史 1", "历史 2", "历史 3", "历史 4", "历史 5"},
	})
}

func TestFitNoTrimWhenWithinBudget(t *testing.T) {
	bundle := budgetBundle()
	fitted, err := Fit(bundle, EstimateTokens(bundle.Text()))
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if fitted.Text() != bundle.Text() || len(fitted.Trimmed) != 0 {
		t.Fatalf("预算刚好时不应裁剪: trimmed=%+v", fitted.Trimmed)
	}
}

func TestFitNeverTrimsLevelZero(t *testing.T) {
	bundle := budgetBundle()
	// 窗口只够 0 级内容 + 一行：应先裁历史，再裁最近交互，但不碰约束/决策
	window := EstimateTokens(baseText(bundle)) + 5
	fitted, err := Fit(bundle, window)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	text := fitted.Text()
	for _, must := range []string{"不得修改数据库结构", "接口必须幂等", "使用 Redis 锁", "实现 X"} {
		if !strings.Contains(text, must) {
			t.Fatalf("0 级内容被裁剪: 缺少 %q\n%s", must, text)
		}
	}
	if EstimateTokens(text) > window {
		t.Fatalf("裁剪后仍超预算: %d > %d", EstimateTokens(text), window)
	}

	// 裁剪必须先 2 级后 4 级；一旦出现 4 级，之前的 2 级必须已裁完
	seenRecent := false
	for _, trim := range fitted.Trimmed {
		if trim.Level == TrimRecent {
			seenRecent = true
		}
		if trim.Level == TrimHistory && seenRecent {
			t.Fatalf("优先级顺序错误: %+v", fitted.Trimmed)
		}
	}
	if !seenRecent {
		t.Fatalf("窗口不足时应裁到最近交互: %+v", fitted.Trimmed)
	}
}

func TestFitErrorsWhenZeroLevelTooLarge(t *testing.T) {
	bundle := Assemble(Request{
		Constraints: []string{strings.Repeat("约束", 100)},
		History:     []string{"历史"},
	})
	if _, err := Fit(bundle, 5); !errors.Is(err, ErrBudgetUnsatisfiable) {
		t.Fatalf("期望 ErrBudgetUnsatisfiable，实际: %v", err)
	}
}

func TestFitHistoryFirst(t *testing.T) {
	bundle := budgetBundle()
	// 0 级 + 最近交互刚好用满预算：只裁掉历史就能满足
	window := EstimateTokens(Assemble(Request{
		Constraints:  []string{"不得修改数据库结构"},
		Requirements: []string{"接口必须幂等"},
		Decisions:    []Decision{{Text: "使用 Redis 锁"}},
		StepInput:    "实现 X",
		Recent:       []string{"最近 1", "最近 2", "最近 3", "最近 4", "最近 5"},
	}).Text())
	fitted, err := Fit(bundle, window)
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	for _, trim := range fitted.Trimmed {
		if trim.Level != TrimHistory {
			t.Fatalf("只应裁历史，实际: %+v", fitted.Trimmed)
		}
	}
	if section, ok := fitted.Section("最近交互"); !ok || len(section.Lines) != 5 {
		t.Fatalf("最近交互不应被裁剪: %+v", section)
	}
}

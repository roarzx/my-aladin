package agentctx

import (
	"strings"
	"testing"
)

func TestAssembleOrder(t *testing.T) {
	bundle := Assemble(Request{
		Constraints:  []string{"不得修改数据库结构"},
		Requirements: []string{"接口必须幂等"},
		Decisions:    []Decision{{Text: "使用 Redis 锁", EvidenceIDs: []string{"e1"}}},
		StepInput:    "实现 X",
		Recent:       []string{"上一轮讨论了 Y"},
		History:      []string{"更早的 Z"},
	})

	want := []string{"不可丢失约束", "需求与验收条件", "已确认决策", "当前步骤输入", "最近交互", "历史"}
	if len(bundle.Sections) != len(want) {
		t.Fatalf("区块数 = %d", len(bundle.Sections))
	}
	for i, name := range want {
		if bundle.Sections[i].Name != name {
			t.Fatalf("第 %d 个区块 = %q, 期望 %q", i, bundle.Sections[i].Name, name)
		}
	}

	text := bundle.Text()
	for _, fragment := range []string{
		"## 不可丢失约束", "- 不得修改数据库结构",
		"## 已确认决策", "- 使用 Redis 锁",
		"## 当前步骤输入", "- 实现 X",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("渲染缺少 %q:\n%s", fragment, text)
		}
	}
	// 顺序：约束在需求之前
	if strings.Index(text, "不可丢失约束") > strings.Index(text, "需求与验收条件") {
		t.Fatal("区块顺序错误")
	}
}

func TestAssembleSkipsEmptySections(t *testing.T) {
	bundle := Assemble(Request{
		Constraints:  []string{"", "   "},
		Requirements: []string{"需要 A"},
	})
	if len(bundle.Sections) != 1 || bundle.Sections[0].Name != "需求与验收条件" {
		t.Fatalf("区块 = %+v", bundle.Sections)
	}
}

func TestSectionLookup(t *testing.T) {
	bundle := Assemble(Request{Constraints: []string{"c"}})
	section, ok := bundle.Section("不可丢失约束")
	if !ok || len(section.Lines) != 1 || section.Lines[0] != "c" {
		t.Fatalf("section = %+v, ok=%v", section, ok)
	}
	if _, ok := bundle.Section("不存在"); ok {
		t.Fatal("不存在的区块不应命中")
	}
}

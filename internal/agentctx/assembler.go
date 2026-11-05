// Package agentctx 负责规划/步骤提示词的上下文装配（D2）。
//
// 装配顺序固定在 dev_spec 3.5；裁剪与预算由 E2 负责，本包不做裁剪。
package agentctx

import "strings"

// Decision 是已确认决策及其证据引用。
type Decision struct {
	Text        string
	EvidenceIDs []string
}

// Request 是装配输入。
type Request struct {
	Constraints  []string
	Requirements []string
	Decisions    []Decision
	StepInput    string
	Recent       []string
	History      []string
}

// Section 是一个装配区块。
type Section struct {
	Name  string
	Lines []string
}

// Bundle 是装配结果（有序）。
type Bundle struct {
	Sections []Section
}

// Assemble 按 3.5 的顺序装配：约束 → 需求 → 决策 → 当前步骤 → 最近交互 → 历史。
func Assemble(req Request) Bundle {
	var sections []Section

	appendSection := func(name string, lines []string) {
		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				filtered = append(filtered, line)
			}
		}
		if len(filtered) > 0 {
			sections = append(sections, Section{Name: name, Lines: filtered})
		}
	}

	appendSection("不可丢失约束", req.Constraints)
	appendSection("需求与验收条件", req.Requirements)

	decisions := make([]string, 0, len(req.Decisions))
	for _, decision := range req.Decisions {
		if strings.TrimSpace(decision.Text) != "" {
			decisions = append(decisions, decision.Text)
		}
	}
	appendSection("已确认决策", decisions)

	if strings.TrimSpace(req.StepInput) != "" {
		appendSection("当前步骤输入", []string{req.StepInput})
	}
	appendSection("最近交互", req.Recent)
	appendSection("历史", req.History)

	return Bundle{Sections: sections}
}

// Text 渲染为 Markdown。
func (b Bundle) Text() string {
	var out strings.Builder
	for i, section := range b.Sections {
		if i > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString("## ")
		out.WriteString(section.Name)
		out.WriteString("\n")
		for _, line := range section.Lines {
			out.WriteString("- ")
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

// Section 按名称查找区块。
func (b Bundle) Section(name string) (Section, bool) {
	for _, section := range b.Sections {
		if section.Name == name {
			return section, true
		}
	}
	return Section{}, false
}

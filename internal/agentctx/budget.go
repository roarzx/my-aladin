package agentctx

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrBudgetUnsatisfiable 表示 0 级内容（约束/验收/决策/当前步骤）本身超出窗口；
// 此时必须报错并暂停，禁止静默丢弃（dev_spec 3.5）。
var ErrBudgetUnsatisfiable = errors.New("agentctx: 预算不足，0 级内容无法保留")

// TrimLevel 对应 dev_spec 3.5 的唯一裁剪优先级表。
//
// 本包只处理装配后的文本区块：工具输出（1）与非依赖步骤摘要（3）
// 在进入装配前就被排除，不产生可裁剪文本；历史（2）与最近交互（4）在装配结果中。
type TrimLevel int

const (
	TrimToolOutput  TrimLevel = 1
	TrimHistory     TrimLevel = 2
	TrimStepSummary TrimLevel = 3
	TrimRecent      TrimLevel = 4
)

// Trim 记录一次裁剪（供 budget.trimmed{level, bytes} 审计事件）。
type Trim struct {
	Level TrimLevel
	Bytes int
}

const (
	sectionRecent  = "最近交互"
	sectionHistory = "历史"
)

// EstimateTokens 是装配阶段的轻量估算：约 4 字节/令牌（按 rune 计）。
//
// 只用于预算裁剪决策，不替代模型侧真实 tokenizer。
func EstimateTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	return (runes + 3) / 4
}

// trimLevel 返回区块的裁剪优先级；0 表示永不裁剪。
func trimLevel(section string) TrimLevel {
	switch section {
	case sectionHistory:
		return TrimHistory
	case sectionRecent:
		return TrimRecent
	default:
		// 约束、需求、决策、当前步骤输入均视为 0 级
		return 0
	}
}

// Fit 按 3.5 的裁剪优先级表把装配结果压缩到 window（令牌）以内。
//
// 只允许从优先级 1 向 4 逐级裁剪；本包实际只会裁 2（历史）和 4（最近交互）。
// 0 级内容永不裁剪；无法保留时返回 ErrBudgetUnsatisfiable，由调用方暂停并告警。
func Fit(bundle Bundle, window int) (Bundle, error) {
	if window <= 0 {
		return Bundle{}, fmt.Errorf("%w: window=%d", ErrBudgetUnsatisfiable, window)
	}

	fitted := Bundle{Sections: cloneSections(bundle.Sections), Trimmed: bundle.Trimmed}
	required := EstimateTokens(fitted.Text())
	if required <= window {
		return fitted, nil
	}

	// 0 级内容本身是否超预算？
	if EstimateTokens(baseText(fitted)) > window {
		return Bundle{}, fmt.Errorf("%w: 0 级内容 %d > %d", ErrBudgetUnsatisfiable,
			EstimateTokens(baseText(fitted)), window)
	}

	// 先裁 2 级（旧历史），再裁 4 级（最近交互），每轮删掉一行并记账。
	for _, level := range []TrimLevel{TrimHistory, TrimRecent} {
		name := sectionName(level)
		for EstimateTokens(fitted.Text()) > window {
			section, ok := fitted.Section(name)
			if !ok || len(section.Lines) == 0 {
				break
			}
			line := section.Lines[len(section.Lines)-1]
			fitted.Trimmed = append(fitted.Trimmed, Trim{Level: level, Bytes: len(line)})
			fitted.removeLastLine(name)
		}
	}

	if EstimateTokens(fitted.Text()) > window {
		return Bundle{}, fmt.Errorf("%w: 裁剪到上限后仍超过 %d", ErrBudgetUnsatisfiable, window)
	}
	return fitted, nil
}

func sectionName(level TrimLevel) string {
	switch level {
	case TrimHistory:
		return sectionHistory
	case TrimRecent:
		return sectionRecent
	default:
		return ""
	}
}

func cloneSections(sections []Section) []Section {
	out := make([]Section, 0, len(sections))
	for _, section := range sections {
		lines := append([]string(nil), section.Lines...)
		out = append(out, Section{Name: section.Name, Lines: lines})
	}
	return out
}

// baseText 只渲染 0 级区块。
func baseText(bundle Bundle) string {
	var base Bundle
	for _, section := range bundle.Sections {
		if trimLevel(section.Name) == 0 {
			base.Sections = append(base.Sections, section)
		}
	}
	return base.Text()
}

func (b *Bundle) removeLastLine(section string) {
	for i := range b.Sections {
		if b.Sections[i].Name != section {
			continue
		}
		b.Sections[i].Lines = b.Sections[i].Lines[:len(b.Sections[i].Lines)-1]
		if len(b.Sections[i].Lines) == 0 {
			b.Sections = append(b.Sections[:i], b.Sections[i+1:]...)
		}
		return
	}
}

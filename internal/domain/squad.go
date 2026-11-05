package domain

import (
	"fmt"
	"time"
)

// ApprovalStep 是审批链中的一步。
type ApprovalStep struct {
	Order      int    `json:"order"`
	ApproverID uint64 `json:"approverId"`
}

// ApprovalPolicy 是小队的审批配置。
type ApprovalPolicy struct {
	Required bool           `json:"required"`
	Chain    []ApprovalStep `json:"chain,omitempty"`
}

// Valid 校验审批链：顺序唯一且从 1 开始连续。
func (p ApprovalPolicy) Valid() error {
	if !p.Required {
		return nil
	}
	if len(p.Chain) == 0 {
		return fmt.Errorf("审批链为空")
	}
	seen := make(map[int]bool, len(p.Chain))
	for _, step := range p.Chain {
		if step.Order < 1 {
			return fmt.Errorf("审批顺序必须从 1 开始")
		}
		if step.ApproverID == 0 {
			return fmt.Errorf("审批人不能为空")
		}
		if seen[step.Order] {
			return fmt.Errorf("审批顺序重复: %d", step.Order)
		}
		seen[step.Order] = true
	}
	for i := 1; i <= len(p.Chain); i++ {
		if !seen[i] {
			return fmt.Errorf("审批顺序不连续，缺少 %d", i)
		}
	}
	return nil
}

// Squad 是小队定义（可修改）。
type Squad struct {
	ID             string
	WorkspaceID    string
	Name           string
	LeaderAgentID  string
	MemberIDs      []string
	Instructions   string
	ApprovalPolicy ApprovalPolicy
	Version        uint64
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SquadSnapshot 是创建 Issue 时冻结的小队语义。
type SquadSnapshot struct {
	ID             string
	TaskID         string
	SquadID        string
	LeaderAgentID  string
	MemberIDs      []string
	Instructions   string
	ApprovalPolicy ApprovalPolicy
	FrozenAt       time.Time
}

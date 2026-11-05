// Package agent 定义 Agent 配置与可调度性规则（B3）。
package agent

import (
	"encoding/json"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Visibility 对应 agents.visibility。
type Visibility string

const (
	Private Visibility = "private"
	Shared  Visibility = "shared"
)

// Valid 校验可见性取值。
func (v Visibility) Valid() error {
	switch v {
	case Private, Shared:
		return nil
	}
	return fmt.Errorf("未知的 Agent 可见性 %q", v)
}

// RuntimeType 对应 agents.runtime_type；v1 只支持 claude_code。
type RuntimeType string

const RuntimeClaudeCode RuntimeType = "claude_code"

// Agent 是执行 Agent 的配置。
type Agent struct {
	ID            string
	WorkspaceID   string
	Name          string
	OwnerID       uint64
	Visibility    Visibility
	RuntimeType   RuntimeType
	MachineID     string
	WorkDir       string
	Model         string
	ThinkingLevel string
	SystemPrompt  string
	CapabilitySet json.RawMessage
	EnvVars       json.RawMessage
	Enabled       bool
}

// VisibleTo 表示该 Agent 是否出现在 userID 的可选列表中。
func (a Agent) VisibleTo(userID uint64) bool {
	switch a.Visibility {
	case Shared:
		return true
	case Private:
		return a.OwnerID == userID
	default:
		return false
	}
}

// SchedulableBy 在创建任务与派发前重新校验（dev_spec 9.5）：
// 共享转私有后，已有的跨用户派发必须明确失败，不能依赖列表缓存。
func (a Agent) SchedulableBy(userID uint64) error {
	if !a.Enabled {
		return errcode.New(errcode.AgentUnavailable, "Agent 已禁用")
	}
	if a.Visibility.Valid() != nil {
		return errcode.New(errcode.AgentUnavailable, "Agent 可见性配置非法")
	}
	if !a.VisibleTo(userID) {
		return errcode.New(errcode.AgentUnavailable, "Agent 仅创建者可调度")
	}
	return nil
}

// TaskRole 是 Agent 在任务中的角色。
func (a Agent) TaskRole(plannerID string) domain.TaskAgentRole {
	if a.ID == plannerID {
		return domain.TaskAgentPlanner
	}
	return domain.TaskAgentMember
}

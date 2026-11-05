package domain

import "fmt"

// TaskAgentRole 对应 task_agents.role。
type TaskAgentRole string

const (
	TaskAgentPlanner TaskAgentRole = "planner"
	TaskAgentMember  TaskAgentRole = "member"
)

// Valid 校验角色取值。
func (r TaskAgentRole) Valid() error {
	switch r {
	case TaskAgentPlanner, TaskAgentMember:
		return nil
	}
	return fmt.Errorf("未知的任务 Agent 角色 %q", r)
}

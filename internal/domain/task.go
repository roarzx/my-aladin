package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// TaskMode 对应 tasks.mode。
type TaskMode string

const (
	ModeOrdinary TaskMode = "ordinary"
	ModeSquad    TaskMode = "squad"
)

// Valid 校验模式取值。
func (m TaskMode) Valid() error {
	switch m {
	case ModeOrdinary, ModeSquad:
		return nil
	}
	return fmt.Errorf("未知的任务模式 %q", m)
}

// TaskStatus 对应 tasks.status（dev_spec 9.1）。
type TaskStatus string

const (
	TaskPending        TaskStatus = "pending"
	TaskPlanConfirming TaskStatus = "plan_confirming"
	TaskExecuting      TaskStatus = "executing"
	TaskPaused         TaskStatus = "paused"
	TaskCancelling     TaskStatus = "cancelling"
	TaskChatting       TaskStatus = "chatting"
	TaskCancelled      TaskStatus = "cancelled"
	TaskFailed         TaskStatus = "failed"
)

var allTaskStatuses = []TaskStatus{
	TaskPending, TaskPlanConfirming, TaskExecuting, TaskPaused,
	TaskCancelling, TaskChatting, TaskCancelled, TaskFailed,
}

// Valid 校验状态取值。
func (s TaskStatus) Valid() error {
	for _, v := range allTaskStatuses {
		if s == v {
			return nil
		}
	}
	return fmt.Errorf("未知的 Task 状态 %q", s)
}

// IsTerminal 表示任务已结束，不再发生自动推进。
func (s TaskStatus) IsTerminal() bool {
	return s == TaskCancelled || s == TaskFailed
}

// Task 是任务聚合根（与 Issue 为同一记录）。
type Task struct {
	ID                string
	WorkspaceID       string
	CreatedBy         uint64
	Name              string
	Mode              TaskMode
	Status            TaskStatus
	Version           uint64
	ActivitySeq       uint64
	PlanBranch        string
	ConfigSnapshot    json.RawMessage
	CancelRequestedAt *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

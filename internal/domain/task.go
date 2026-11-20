package domain

import (
	"encoding/json"
	"fmt"
	"strings"
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
	// TaskCompleted 表示计划已全部完成；仍可追加新计划继续对话式修改。
	TaskCompleted TaskStatus = "completed"
	TaskCancelled TaskStatus = "cancelled"
	TaskFailed    TaskStatus = "failed"
)

var allTaskStatuses = []TaskStatus{
	TaskPending, TaskPlanConfirming, TaskExecuting, TaskPaused,
	TaskCancelling, TaskChatting, TaskCompleted, TaskCancelled, TaskFailed,
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

// reportedStatusAliases 是 Agent 上报时可用的别名。
//
// Agent 面对的是「进行中 / 阻塞 / 完成」，不必知道库里的枚举名。
var reportedStatusAliases = map[string]TaskStatus{
	"in_progress": TaskExecuting,
	"running":     TaskExecuting,
	"blocked":     TaskPaused,
	"waiting":     TaskPaused,
	"done":        TaskCompleted,
	"canceled":    TaskCancelled,
}

// ParseReportedStatus 解析 Agent 上报的状态名，接受别名与库内枚举名。
func ParseReportedStatus(raw string) (TaskStatus, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return "", fmt.Errorf("状态不能为空")
	}
	if alias, ok := reportedStatusAliases[key]; ok {
		return alias, nil
	}
	status := TaskStatus(key)
	if err := status.Valid(); err != nil {
		return "", err
	}
	return status, nil
}

// Task 是任务聚合根（与 Issue 为同一记录）。
type Task struct {
	ID          string
	WorkspaceID string
	CreatedBy   uint64
	Name        string
	// Requirement 是需求正文（CLI `issue create --requirement-content` 写入）。
	Requirement string
	// InformDoc 是告知规则/补充文档。
	InformDoc         string
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

// ReportStatus 接受 Agent 通过 CLI 上报的状态（`issue update-status`）。
//
// 新模型下 Issue 状态由 Agent 驱动，平台不再用 taskTransitions 约束它：
// 这里只做取值校验与「终态不可复活」的保护。已完成/已暂停可以回到进行中
// （用户追问后继续干），已取消/已失败则保持终态，避免误复活。
func (t *Task) ReportStatus(next TaskStatus) error {
	if err := next.Valid(); err != nil {
		return err
	}
	if t.Status == next {
		return nil
	}
	if t.Status.IsTerminal() {
		return fmt.Errorf("任务已处于终态 %q，不能再上报为 %q", t.Status, next)
	}
	t.Status = next
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	return nil
}

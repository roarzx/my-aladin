package domain

import "fmt"

// StepStatus 对应 plan_steps.status（dev_spec 9.1）。
//
// 注意：Step 不包含 dispatched / running —— 传输状态由 Session 承担。
type StepStatus string

const (
	StepPending     StepStatus = "pending"
	StepExecuting   StepStatus = "executing"
	StepCompleted   StepStatus = "completed"
	StepFailed      StepStatus = "failed"
	StepCancelled   StepStatus = "cancelled"
	StepReconciling StepStatus = "reconciling"
)

var allStepStatuses = []StepStatus{
	StepPending, StepExecuting, StepCompleted, StepFailed, StepCancelled, StepReconciling,
}

// Valid 校验状态取值。
func (s StepStatus) Valid() error {
	for _, v := range allStepStatuses {
		if s == v {
			return nil
		}
	}
	return fmt.Errorf("未知的 Step 状态 %q", s)
}

// IsTerminal 表示步骤已到达终态；失败可通过显式 Retry 事件重新开始。
func (s StepStatus) IsTerminal() bool {
	return s == StepCompleted || s == StepFailed || s == StepCancelled
}

// Step 是计划中的一步。
type Step struct {
	ID       string
	PlanID   string
	Ordinal  int
	Title    string
	Body     string
	AgentRef string
	Status   StepStatus
	Version  uint64
}

package domain

import "testing"

func TestTaskStatusValid(t *testing.T) {
	for _, s := range allTaskStatuses {
		if err := s.Valid(); err != nil {
			t.Errorf("%q 应为合法状态: %v", s, err)
		}
	}
	if err := TaskStatus("dispatched").Valid(); err == nil {
		t.Error("未知状态应被拒绝")
	}
}

func TestTaskStatusTerminal(t *testing.T) {
	terminal := map[TaskStatus]bool{
		TaskCancelled:  true,
		TaskFailed:     true,
		TaskPending:    false,
		TaskExecuting:  false,
		TaskCancelling: false,
		TaskChatting:   false,
	}
	for s, want := range terminal {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%q.IsTerminal() = %v, 期望 %v", s, got, want)
		}
	}
}

func TestPlanStatus(t *testing.T) {
	for _, s := range allPlanStatuses {
		if err := s.Valid(); err != nil {
			t.Errorf("%q 应为合法状态", s)
		}
	}
	if err := PlanStatus("cancelled").Valid(); err == nil {
		t.Error("Plan 没有 cancelled 状态")
	}
	if !PlanCompleted.IsTerminal() || !PlanStopped.IsTerminal() {
		t.Error("completed/stopped 应为终态")
	}
	if PlanExecuting.IsTerminal() {
		t.Error("executing 不应是终态")
	}
}

func TestStepStatus(t *testing.T) {
	for _, s := range allStepStatuses {
		if err := s.Valid(); err != nil {
			t.Errorf("%q 应为合法状态", s)
		}
	}
	if err := StepStatus("running").Valid(); err == nil {
		t.Error("Step 不应包含 running（传输状态由 Session 承担）")
	}
	if !StepCompleted.IsTerminal() || !StepFailed.IsTerminal() || !StepCancelled.IsTerminal() {
		t.Error("completed/failed/cancelled 应为终态")
	}
	if StepReconciling.IsTerminal() {
		t.Error("reconciling 仍需对账，不是终态")
	}
}

func TestAttemptAndErrorClass(t *testing.T) {
	for _, s := range allAttemptStatuses {
		if err := s.Valid(); err != nil {
			t.Errorf("%q 应为合法 Attempt 状态", s)
		}
	}
	if AttemptUnknown.IsTerminal() {
		t.Error("unknown 需要对账，不是终态")
	}
	for _, c := range allErrorClasses {
		if err := c.Valid(); err != nil {
			t.Errorf("%q 应为合法分类", c)
		}
	}
	if err := ErrorClass("other").Valid(); err == nil {
		t.Error("未知分类应被拒绝")
	}
	if !ErrNetwork.Retryable() || ErrVerification.Retryable() || ErrPermission.Retryable() {
		t.Error("重试策略与 3.4 不一致")
	}
}

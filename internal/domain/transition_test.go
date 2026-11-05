package domain

import "testing"

type taskCase struct {
	name   string
	from   TaskStatus
	ev     Event
	want   TaskStatus
	reject bool
}

func TestTaskTransitions(t *testing.T) {
	cases := []taskCase{
		{name: "created", from: "", ev: EvTaskCreated, want: TaskPending},
		{name: "submitted", from: TaskPending, ev: EvPlanSubmitted, want: TaskPlanConfirming},
		{name: "planning failed", from: TaskPending, ev: EvPlanningFailed, want: TaskFailed},
		{name: "confirmed and started", from: TaskPlanConfirming, ev: EvPlanConfirmedAndStarted, want: TaskExecuting},
		{name: "revised stays", from: TaskPlanConfirming, ev: EvPlanRevised, want: TaskPlanConfirming},
		{name: "step failed", from: TaskExecuting, ev: EvStepFailed, want: TaskPaused},
		{name: "step reconciling", from: TaskExecuting, ev: EvStepReconciling, want: TaskPaused},
		{name: "plan completed", from: TaskExecuting, ev: EvPlanCompleted, want: TaskChatting},
		{name: "retried", from: TaskPaused, ev: EvStepRetried, want: TaskExecuting},
		{name: "cancel from executing", from: TaskExecuting, ev: EvTaskCancelRequested, want: TaskCancelling},
		{name: "cancel from paused", from: TaskPaused, ev: EvTaskCancelRequested, want: TaskCancelling},
		{name: "cancel confirmed", from: TaskCancelling, ev: EvTaskCancelConfirmed, want: TaskCancelled},
		{name: "cancel timeout", from: TaskCancelling, ev: EvTaskCancelTimeout, want: TaskCancelled},
		{name: "appended", from: TaskChatting, ev: EvPlanAppended, want: TaskPlanConfirming},

		{name: "cancelled is terminal", from: TaskCancelled, ev: EvPlanSubmitted, reject: true},
		{name: "failed is terminal", from: TaskFailed, ev: EvTaskCancelRequested, reject: true},
		{name: "cannot complete before start", from: TaskPending, ev: EvPlanCompleted, reject: true},
		{name: "cannot complete while confirming", from: TaskPlanConfirming, ev: EvPlanCompleted, reject: true},
		{name: "cannot resubmit", from: TaskExecuting, ev: EvPlanSubmitted, reject: true},
		{name: "must be created first", from: "", ev: EvPlanSubmitted, reject: true},
		{name: "unknown event", from: TaskPending, ev: Event("nope"), reject: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{Status: tc.from}
			err := task.Apply(tc.ev)
			if tc.reject {
				if err == nil {
					t.Fatalf("期望拒绝，实际转换为 %q", task.Status)
				}
				if task.Status != tc.from {
					t.Fatalf("拒绝后状态被改写: %q", task.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功: %v", err)
			}
			if task.Status != tc.want {
				t.Fatalf("状态 = %q, 期望 %q", task.Status, tc.want)
			}
			if task.Version != 1 {
				t.Fatalf("Version = %d, 期望 1", task.Version)
			}
		})
	}
}

func TestTaskApplyIncrementsVersion(t *testing.T) {
	task := &Task{}
	if err := task.Apply(EvTaskCreated); err != nil {
		t.Fatal(err)
	}
	if err := task.Apply(EvPlanSubmitted); err != nil {
		t.Fatal(err)
	}
	if task.Version != 2 {
		t.Fatalf("Version = %d, 期望 2", task.Version)
	}
	if task.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 应被设置")
	}
}

func TestPlanTransitions(t *testing.T) {
	cases := []struct {
		name   string
		from   PlanStatus
		ev     Event
		want   PlanStatus
		reject bool
	}{
		{name: "proposed stays idle", from: PlanIdle, ev: EvPlanProposed, want: PlanIdle},
		{name: "started", from: PlanIdle, ev: EvPlanStarted, want: PlanExecuting},
		{name: "stopped while idle", from: PlanIdle, ev: EvPlanStopped, want: PlanStopped},
		{name: "step started", from: PlanExecuting, ev: EvStepStarted, want: PlanExecuting},
		{name: "finished", from: PlanExecuting, ev: EvPlanFinished, want: PlanCompleted},
		{name: "step failed", from: PlanExecuting, ev: EvStepFailed, want: PlanPaused},
		{name: "reconciling", from: PlanExecuting, ev: EvStepReconciling, want: PlanPaused},
		{name: "stopped while executing", from: PlanExecuting, ev: EvPlanStopped, want: PlanStopped},
		{name: "retried", from: PlanPaused, ev: EvStepRetried, want: PlanExecuting},
		{name: "stopped while paused", from: PlanPaused, ev: EvPlanStopped, want: PlanStopped},

		{name: "completed is terminal", from: PlanCompleted, ev: EvStepStarted, reject: true},
		{name: "stopped is terminal", from: PlanStopped, ev: EvPlanStarted, reject: true},
		{name: "cannot finish while idle", from: PlanIdle, ev: EvPlanFinished, reject: true},
		{name: "cannot finish while paused", from: PlanPaused, ev: EvPlanFinished, reject: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &Plan{Status: tc.from}
			err := plan.Apply(tc.ev)
			if tc.reject {
				if err == nil {
					t.Fatalf("期望拒绝，实际转换为 %q", plan.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功: %v", err)
			}
			if plan.Status != tc.want {
				t.Fatalf("状态 = %q, 期望 %q", plan.Status, tc.want)
			}
		})
	}
}

func TestStepTransitions(t *testing.T) {
	cases := []struct {
		name   string
		from   StepStatus
		ev     Event
		want   StepStatus
		reject bool
	}{
		{name: "selected", from: StepPending, ev: EvStepSelected, want: StepExecuting},
		{name: "done", from: StepExecuting, ev: EvStepDone, want: StepCompleted},
		{name: "retryable failure stays executing", from: StepExecuting, ev: EvAttemptFailedRetryable, want: StepExecuting},
		{name: "terminal failure", from: StepExecuting, ev: EvStepFailed, want: StepFailed},
		{name: "dispatch failure retry", from: StepExecuting, ev: EvDispatchFailedRetryable, want: StepExecuting},
		{name: "result unknown", from: StepExecuting, ev: EvResultUnknown, want: StepReconciling},
		{name: "reconciled completed", from: StepReconciling, ev: EvReconcileCompleted, want: StepCompleted},
		{name: "reconciled not executed", from: StepReconciling, ev: EvReconcileNotExecuted, want: StepFailed},
		{name: "reconciled not executed retryable", from: StepReconciling, ev: EvReconcileNotExecutedRetryable, want: StepExecuting},
		{name: "manual retry", from: StepFailed, ev: EvStepRetried, want: StepExecuting},
		{name: "cancel confirmed", from: StepExecuting, ev: EvTaskCancelConfirmed, want: StepCancelled},
		{name: "cancel timeout", from: StepExecuting, ev: EvTaskCancelTimeout, want: StepCancelled},
		{name: "cancel while reconciling", from: StepReconciling, ev: EvTaskCancelConfirmed, want: StepCancelled},

		{name: "completed is terminal", from: StepCompleted, ev: EvStepRetried, reject: true},
		{name: "cancelled is terminal", from: StepCancelled, ev: EvStepRetried, reject: true},
		{name: "no retry while reconciling", from: StepReconciling, ev: EvStepRetried, reject: true},
		{name: "cannot finish pending", from: StepPending, ev: EvStepDone, reject: true},
		{name: "cannot finish failed", from: StepFailed, ev: EvStepDone, reject: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := &Step{Status: tc.from}
			err := step.Apply(tc.ev)
			if tc.reject {
				if err == nil {
					t.Fatalf("期望拒绝，实际转换为 %q", step.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功: %v", err)
			}
			if step.Status != tc.want {
				t.Fatalf("状态 = %q, 期望 %q", step.Status, tc.want)
			}
		})
	}
}

package domain

import (
	"fmt"
	"time"
)

// 以下三张表是 dev_spec 9.1 的直接映射。
//
// 条件类事件（是否可自动重试、预算是否用尽）由调用方在发事件前判定：
// 可自动重试用 attempt.failed_retryable，不可重试用 step.failed。
var (
	taskTransitions = map[TaskStatus]map[Event]TaskStatus{
		TaskPending: {
			EvPlanSubmitted:  TaskPlanConfirming,
			EvPlanningFailed: TaskFailed,
		},
		TaskPlanConfirming: {
			EvPlanConfirmedAndStarted: TaskExecuting,
			EvPlanRevised:             TaskPlanConfirming,
		},
		TaskExecuting: {
			EvStepFailed:          TaskPaused,
			EvStepReconciling:     TaskPaused,
			EvPlanCompleted:       TaskChatting,
			EvTaskCancelRequested: TaskCancelling,
		},
		TaskPaused: {
			EvStepRetried:         TaskExecuting,
			EvTaskCancelRequested: TaskCancelling,
		},
		TaskCancelling: {
			EvTaskCancelConfirmed: TaskCancelled,
			EvTaskCancelTimeout:   TaskCancelled,
		},
		TaskChatting: {
			EvPlanAppended: TaskPlanConfirming,
		},
	}

	planTransitions = map[PlanStatus]map[Event]PlanStatus{
		PlanIdle: {
			EvPlanProposed: PlanIdle,
			EvPlanStarted:  PlanExecuting,
			EvPlanStopped:  PlanStopped,
		},
		PlanExecuting: {
			EvStepStarted:     PlanExecuting,
			EvPlanFinished:    PlanCompleted,
			EvStepFailed:      PlanPaused,
			EvStepReconciling: PlanPaused,
			EvPlanStopped:     PlanStopped,
		},
		PlanPaused: {
			EvStepRetried: PlanExecuting,
			EvPlanStopped: PlanStopped,
		},
	}

	stepTransitions = map[StepStatus]map[Event]StepStatus{
		StepPending: {
			EvStepSelected: StepExecuting,
			EvStepFailed:   StepFailed, // 派发前授权校验失败（9.5）
		},
		StepExecuting: {
			EvStepDone:                StepCompleted,
			EvAttemptFailedRetryable:  StepExecuting,
			EvStepFailed:              StepFailed,
			EvDispatchFailedRetryable: StepExecuting,
			EvResultUnknown:           StepReconciling,
			EvTaskCancelConfirmed:     StepCancelled,
			EvTaskCancelTimeout:       StepCancelled,
		},
		StepReconciling: {
			EvReconcileCompleted:            StepCompleted,
			EvReconcileNotExecutedRetryable: StepExecuting,
			EvReconcileNotExecuted:          StepFailed,
			EvTaskCancelConfirmed:           StepCancelled,
			EvTaskCancelTimeout:             StepCancelled,
		},
		StepFailed: {
			EvStepRetried: StepExecuting,
		},
	}
)

func resolve[S ~string](transitions map[S]map[Event]S, from S, ev Event, kind string) (S, error) {
	var zero S
	events, ok := transitions[from]
	if !ok {
		return zero, fmt.Errorf("%s: 不允许从 %q 经 %q 转换", kind, from, ev)
	}
	next, ok := events[ev]
	if !ok {
		return zero, fmt.Errorf("%s: 不允许从 %q 经 %q 转换", kind, from, ev)
	}
	return next, nil
}

// CanTransition 校验事件是否被当前状态接受。
func (t *Task) CanTransition(ev Event) error {
	if t.Status == "" && ev == EvTaskCreated {
		return nil
	}
	_, err := resolve(taskTransitions, t.Status, ev, "task")
	return err
}

// Apply 校验并应用事件；成功后递增 Version。
func (t *Task) Apply(ev Event) error {
	if t.Status == "" {
		if ev != EvTaskCreated {
			return fmt.Errorf("task: 未创建的任务只能接受 %q，收到 %q", EvTaskCreated, ev)
		}
		t.Status = TaskPending
		t.Version++
		t.UpdatedAt = time.Now().UTC()
		return nil
	}
	next, err := resolve(taskTransitions, t.Status, ev, "task")
	if err != nil {
		return err
	}
	t.Status = next
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// CanTransition 校验事件是否被当前状态接受。
func (p *Plan) CanTransition(ev Event) error {
	_, err := resolve(planTransitions, p.Status, ev, "plan")
	return err
}

// Apply 校验并应用事件；成功后递增 Version。
func (p *Plan) Apply(ev Event) error {
	next, err := resolve(planTransitions, p.Status, ev, "plan")
	if err != nil {
		return err
	}
	p.Status = next
	p.Version++
	return nil
}

// CanTransition 校验事件是否被当前状态接受。
func (s *Step) CanTransition(ev Event) error {
	_, err := resolve(stepTransitions, s.Status, ev, "step")
	return err
}

// Apply 校验并应用事件；成功后递增 Version。
func (s *Step) Apply(ev Event) error {
	next, err := resolve(stepTransitions, s.Status, ev, "step")
	if err != nil {
		return err
	}
	s.Status = next
	s.Version++
	return nil
}

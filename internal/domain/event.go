package domain

// Event 驱动状态机；名称与 dev_spec 9.1 的"事件"列一致。
//
// 同一事件名可能出现在不同实体上（如 step.failed 同时驱动 Task / Plan / Step），
// 但每个实体的转换表是独立的。
type Event string

const (
	// Task
	EvTaskCreated             Event = "task.created"
	EvPlanSubmitted           Event = "plan.submitted"
	EvPlanningFailed          Event = "planning.failed"
	EvPlanConfirmedAndStarted Event = "plan.confirmed_and_started"
	EvPlanRevised             Event = "plan.revised"
	EvStepFailed              Event = "step.failed"
	EvStepReconciling         Event = "step.reconciling"
	EvPlanCompleted           Event = "plan.completed"
	EvStepRetried             Event = "step.retried"
	EvTaskCancelRequested     Event = "task.cancel_requested"
	EvTaskCancelConfirmed     Event = "task.cancel_confirmed"
	EvTaskCancelTimeout       Event = "task.cancel_timeout"
	EvPlanAppended            Event = "plan.appended"

	// Plan
	EvPlanProposed Event = "plan.proposed"
	EvPlanStarted  Event = "plan.started"
	EvStepStarted  Event = "step.started"
	EvPlanFinished Event = "plan.finished"
	EvPlanStopped  Event = "plan.stopped"

	// Step
	EvStepSelected                  Event = "step.selected"
	EvStepDone                      Event = "step.done"
	EvAttemptFailedRetryable        Event = "attempt.failed_retryable"
	EvDispatchFailedRetryable       Event = "dispatch.failed_retryable"
	EvResultUnknown                 Event = "attempt.result_unknown"
	EvReconcileCompleted            Event = "reconcile.completed"
	EvReconcileNotExecuted          Event = "reconcile.not_executed"
	EvReconcileNotExecutedRetryable Event = "reconcile.not_executed_retryable"
)

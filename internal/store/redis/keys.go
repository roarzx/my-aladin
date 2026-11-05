package redisstore

// Keys 集中生成 Redis 键名，对应 dev_spec 9.4。
type Keys struct{}

// Plan 是任务的计划推进点：Hash{status, revision, current_step_id}。
func (Keys) Plan(taskID string) string { return "plan:" + taskID }

// TaskLock 是任务执行锁（PlanRunner / SquadRunner 互斥）。
func (Keys) TaskLock(taskID string) string { return "lock:task:" + taskID }

// Lease 是执行尝试的租约，值为 epoch。
func (Keys) Lease(attemptID string) string { return "lease:attempt:" + attemptID }

// RecoveryDirty 是待对账队列（ZSet，member=task_id，score=进入时间）。
func (Keys) RecoveryDirty() string { return "recovery:dirty" }

// Backpressure 是会话背压计数与暂停标记。
func (Keys) Backpressure(sessionID string) string { return "bp:" + sessionID }

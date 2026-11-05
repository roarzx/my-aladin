package daemon

import (
	"context"
	"sync"
)

// ActiveExecution 是心跳里上报的执行与代次（dev_spec 9.7 heartbeat payload）。
type ActiveExecution struct {
	ExecutionID string `json:"execution_id"`
	LeaseEpoch  uint64 `json:"lease_epoch"`
}

type runningEntry struct {
	epoch  uint64
	cancel context.CancelFunc
}

// RunningExecutions 记录本机正在执行的 execution，用于心跳续约与停止命令（E4/E9）。
type RunningExecutions struct {
	mu    sync.Mutex
	items map[string]runningEntry
}

// NewRunningExecutions 构造注册表。
func NewRunningExecutions() *RunningExecutions {
	return &RunningExecutions{items: make(map[string]runningEntry)}
}

// Start 登记一个执行；返回注销函数。
func (r *RunningExecutions) Start(executionID string, epoch uint64, cancel context.CancelFunc) func() {
	r.mu.Lock()
	r.items[executionID] = runningEntry{epoch: epoch, cancel: cancel}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.items, executionID)
		r.mu.Unlock()
	}
}

// Cancel 取消指定执行；返回是否存在。
func (r *RunningExecutions) Cancel(executionID string) bool {
	r.mu.Lock()
	entry, ok := r.items[executionID]
	r.mu.Unlock()
	if ok && entry.cancel != nil {
		entry.cancel()
	}
	return ok
}

// Snapshot 返回当前活跃执行的快照。
func (r *RunningExecutions) Snapshot() []ActiveExecution {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ActiveExecution, 0, len(r.items))
	for id, entry := range r.items {
		out = append(out, ActiveExecution{ExecutionID: id, LeaseEpoch: entry.epoch})
	}
	return out
}

// Package runtime 定义控制面视角的执行通道（dev_spec 5.5）。
//
// 真实实现是 WS 传输层，测试实现是 internal/testkit/fakeruntime。
package runtime

import "context"

// DispatchRequest 是一次派发请求；ExecutionID 由服务端在派发前固定。
type DispatchRequest struct {
	ExecutionID string
	MachineID   string
	TaskID      string
	StepID      string
	AttemptNo   uint32
	// LeaseEpoch 是本次尝试的执行租约代次（心跳续约时校验）。
	LeaseEpoch uint64
	Branch     string
	Prompt     string
	// WorkDir 是 Agent 在机器上的工作目录（绝对路径）。
	WorkDir string
	// RepoURL 是任务绑定的仓库地址，可为空。
	RepoURL string
	// RepoDefaultBranch 是仓库默认分支，如 main。
	RepoDefaultBranch string
	// SystemPrompt 是 Agent 的系统提示词。
	SystemPrompt string
	// TaskTitle 是任务名称。
	TaskTitle string
	// StepTitle 是当前步骤标题。
	StepTitle string
}

// ExecutionState 是执行快照状态。
type ExecutionState string

const (
	StateDispatched ExecutionState = "dispatched"
	StateRunning    ExecutionState = "running"
	StateCompleted  ExecutionState = "completed"
	StateFailed     ExecutionState = "failed"
	StateCancelled  ExecutionState = "cancelled"
	StateUnknown    ExecutionState = "unknown"
)

// ExecutionSnapshot 是 Inspect 的返回值。
type ExecutionSnapshot struct {
	ExecutionID string
	State       ExecutionState
	ErrorClass  string
	Detail      string
}

// CapabilitySet 描述执行侧能力；能力缺失时必须显式报错。
type CapabilitySet struct {
	Attachments bool
	Git         bool
	FlowNode    bool
}

// Runtime 是控制面 → 执行面的唯一通道。
type Runtime interface {
	Dispatch(ctx context.Context, req DispatchRequest) (executionID string, err error)
	Inspect(ctx context.Context, executionID string) (ExecutionSnapshot, error)
	Cancel(ctx context.Context, executionID string) (acknowledged bool, err error)
	Capabilities() CapabilitySet
}

// Package runtime 定义控制面视角的执行通道。
//
// 新模型里一个 Issue 由 Agent 在长会话中自主推进：控制面只负责把一条消息
// 交给 Agent 并收集回执，不再有步骤、尝试与租约。
package runtime

import "context"

// DispatchRequest 是一次「开始一轮」的请求。
type DispatchRequest struct {
	ExecutionID string
	MachineID   string
	// IssueID 是任务（Issue）ID，Agent 用它调用平台 CLI。
	IssueID string
	AgentID string
	// SessionID 是服务端会话行 ID；ACPSessionID 非空时表示可复用上一轮会话。
	SessionID    string
	ACPSessionID string
	Prompt       string
	SystemPrompt string
	// WorkDir 是 Agent 在机器上的工作目录（绝对路径）。
	WorkDir string
	// RepoURL 是任务绑定的仓库地址，可为空。
	RepoURL string
	// RepoDefaultBranch 是仓库默认分支，如 main。
	RepoDefaultBranch string
	// Branch 是本 Issue 的工作分支，可为空。
	Branch     string
	IssueTitle string
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
	Cancel(ctx context.Context, executionID string) (acknowledged bool, err error)
	Capabilities() CapabilitySet
}

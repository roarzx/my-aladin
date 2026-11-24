package domain

import "time"

// RunStatus 是一轮执行的终态（issue_runs.status）。
type RunStatus string

const (
	RunDispatched RunStatus = "dispatched"
	RunRunning    RunStatus = "running"
	RunCompleted  RunStatus = "completed"
	RunFailed     RunStatus = "failed"
	RunCancelled  RunStatus = "cancelled"
)

// IssueRun 是一次「把提示词交给 Agent」的执行记录。
//
// 新模型下没有计划与步骤：用户发一条消息、或点了开始，就产生一轮执行；
// Agent 在这一轮里自己决定做什么，并用 CLI 上报 Issue 状态。
type IssueRun struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"taskId"`
	AgentID     string    `json:"agentId"`
	SessionID   string    `json:"sessionId,omitempty"`
	ExecutionID string    `json:"executionId"`
	Status      RunStatus `json:"status"`
	Reply       string    `json:"reply,omitempty"`
	ErrorClass  string    `json:"errorClass,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	// 用量快照：由 turn_done 回填，供统计与看板使用。
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	Model        string    `json:"model,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

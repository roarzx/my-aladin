package domain

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// FlowRunStatus 对应 flow_runs.status。
type FlowRunStatus string

const (
	FlowRunQueued    FlowRunStatus = "queued"
	FlowRunRunning   FlowRunStatus = "running"
	FlowRunSucceeded FlowRunStatus = "succeeded"
	FlowRunFailed    FlowRunStatus = "failed"
	FlowRunCancelled FlowRunStatus = "cancelled"
)

// Valid 校验状态取值。
func (s FlowRunStatus) Valid() error {
	switch s {
	case FlowRunQueued, FlowRunRunning, FlowRunSucceeded, FlowRunFailed, FlowRunCancelled:
		return nil
	default:
		return fmt.Errorf("未知的 FlowRun 状态 %q", s)
	}
}

// IsTerminal 表示流程已结束。
func (s FlowRunStatus) IsTerminal() bool {
	return s == FlowRunSucceeded || s == FlowRunFailed || s == FlowRunCancelled
}

// FlowNodeStatus 对应 flow_node_runs.status。
type FlowNodeStatus string

const (
	FlowNodePending   FlowNodeStatus = "pending"
	FlowNodeExecuting FlowNodeStatus = "executing"
	FlowNodeCompleted FlowNodeStatus = "completed"
	FlowNodeFailed    FlowNodeStatus = "failed"
	FlowNodeSkipped   FlowNodeStatus = "skipped"
	FlowNodeStale     FlowNodeStatus = "stale" // 上游重跑后必须重算
)

// Valid 校验节点状态。
func (s FlowNodeStatus) Valid() error {
	switch s {
	case FlowNodePending, FlowNodeExecuting, FlowNodeCompleted,
		FlowNodeFailed, FlowNodeSkipped, FlowNodeStale:
		return nil
	default:
		return fmt.Errorf("未知的 FlowNode 状态 %q", s)
	}
}

// FlowRun 是一次验收流程运行。
type FlowRun struct {
	ID               string
	TaskID           string
	TemplateID       string
	TemplateVersion  int
	TemplateSnapshot json.RawMessage
	Inputs           json.RawMessage
	Status           FlowRunStatus
	Version          uint64
}

// FlowNodeRun 是运行中的一个节点。
type FlowNodeRun struct {
	ID             string
	RunID          string
	NodeKey        string
	Ordinal        int
	Status         FlowNodeStatus
	AttemptNo      int
	LeaseOwner     string
	LeaseEpoch     uint64
	LeaseExpiresAt sql.NullTime
	Result         json.RawMessage
	UpdatedAt      time.Time
}

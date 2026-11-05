package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Result 是节点执行结果。
type Result struct {
	Success bool
	Payload json.RawMessage
}

// Node 是 Server 侧节点执行器（environment_prepare / mars_acceptance）。
type Node interface {
	Key() NodeKey
	Execute(ctx context.Context, run domain.FlowRun, node domain.FlowNodeRun) (Result, error)
}

// EnvironmentPrepareNode 准备执行环境（v1：无副作用占位）。
type EnvironmentPrepareNode struct{}

func (EnvironmentPrepareNode) Key() NodeKey { return NodeEnvironmentPrepare }

func (EnvironmentPrepareNode) Execute(context.Context, domain.FlowRun, domain.FlowNodeRun) (Result, error) {
	return Result{Success: true, Payload: json.RawMessage(`{"prepared":true}`)}, nil
}

// MarsAcceptanceNode 执行验收（v1：按输入记录通过，真实命令由后续阶段接入）。
type MarsAcceptanceNode struct{}

func (MarsAcceptanceNode) Key() NodeKey { return NodeMarsAcceptance }

func (MarsAcceptanceNode) Execute(_ context.Context, run domain.FlowRun, _ domain.FlowNodeRun) (Result, error) {
	payload, _ := json.Marshal(map[string]any{"accepted": true, "inputs": json.RawMessage(run.Inputs)})
	return Result{Success: true, Payload: payload}, nil
}

// DefaultExecutors 返回固定节点执行器。
func DefaultExecutors() map[NodeKey]Node {
	return map[NodeKey]Node{
		NodeEnvironmentPrepare: EnvironmentPrepareNode{},
		NodeMarsAcceptance:     MarsAcceptanceNode{},
	}
}

// FlowRunner 按固定顺序推进 Server 侧节点（G3）。agent_prompt 由 G4 领取。
type FlowRunner struct {
	DB    *sql.DB
	Nodes map[NodeKey]Node
	Log   *slog.Logger
}

// RunOnce 推进运行中第一个可执行的 Server 节点。
func (r *FlowRunner) RunOnce(ctx context.Context, runID string) error {
	run, err := mysqlstore.GetFlowRun(ctx, r.DB, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "流程运行不存在")
	}
	if err != nil {
		return err
	}
	if run.Status.IsTerminal() {
		return nil
	}
	if run.Status == domain.FlowRunQueued {
		version, err := mysqlstore.UpdateFlowRunStatus(ctx, r.DB, run.ID,
			domain.FlowRunQueued, domain.FlowRunRunning, run.Version)
		if err != nil {
			return errcode.New(errcode.Conflict, "流程已被其他 Runner 接管")
		}
		run.Status, run.Version = domain.FlowRunRunning, version
	}

	nodes, err := mysqlstore.ListFlowNodeRuns(ctx, r.DB, run.ID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		switch node.Status {
		case domain.FlowNodeCompleted, domain.FlowNodeSkipped:
			continue
		case domain.FlowNodeStale:
			node.Status = domain.FlowNodePending // 重跑后按 pending 重新执行
		case domain.FlowNodeExecuting:
			// 已由机器领取，等待完成
			return nil
		case domain.FlowNodeFailed:
			// 失败节点需人工 Rerun，RunOnce 不自动重跑
			return nil
		}
		if node.NodeKey == string(NodeAgentPrompt) {
			// agent_prompt 由机器通过 Claim 领取；不阻塞前序 Server 节点
			return nil
		}
		if err := r.executeNode(ctx, run, node); err != nil {
			return err
		}
		return r.RunOnce(ctx, run.ID)
	}
	// 走到这里表示所有节点都已 completed/skipped
	if _, err := mysqlstore.UpdateFlowRunStatus(ctx, r.DB, run.ID,
		domain.FlowRunRunning, domain.FlowRunSucceeded, run.Version); err != nil {
		return errcode.New(errcode.Conflict, "流程状态已变化")
	}
	return nil
}

func (r *FlowRunner) executeNode(ctx context.Context, run domain.FlowRun, node domain.FlowNodeRun) error {
	executor, ok := r.Nodes[NodeKey(node.NodeKey)]
	if !ok {
		r.fail(ctx, node, fmt.Sprintf("节点 %s 没有执行器", node.NodeKey))
		return errcode.Newf(errcode.InternalError, "节点 %s 没有执行器", node.NodeKey)
	}
	result, err := executor.Execute(ctx, run, node)
	if err != nil || !result.Success {
		message := "节点执行失败"
		if err != nil {
			message = err.Error()
		}
		r.fail(ctx, node, message)
		return errcode.Newf(errcode.VerificationIncomplete, "节点 %s 失败: %s", node.NodeKey, message)
	}
	if _, err := mysqlstore.UpdateFlowNodeStatus(ctx, r.DB, node.ID,
		node.Status, domain.FlowNodeCompleted, result.Payload); err != nil {
		return err
	}
	return nil
}

func (r *FlowRunner) fail(ctx context.Context, node domain.FlowNodeRun, message string) {
	_, _ = mysqlstore.UpdateFlowNodeStatus(ctx, r.DB, node.ID,
		node.Status, domain.FlowNodeFailed, mustJSON(map[string]any{"error": message}))
	if run, err := mysqlstore.GetFlowRun(ctx, r.DB, node.RunID); err == nil &&
		run.Status == domain.FlowRunRunning {
		_, _ = mysqlstore.UpdateFlowRunStatus(ctx, r.DB, run.ID,
			domain.FlowRunRunning, domain.FlowRunFailed, run.Version)
	}
	r.log().Warn("流程节点失败", "node", node.ID, "key", node.NodeKey, "err", message)
}

func (r *FlowRunner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

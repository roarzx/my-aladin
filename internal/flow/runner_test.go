//go:build integration

package flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/flow"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

type failingNode struct{}

func (failingNode) Key() flow.NodeKey { return flow.NodeEnvironmentPrepare }

func (failingNode) Execute(context.Context, domain.FlowRun, domain.FlowNodeRun) (flow.Result, error) {
	return flow.Result{Success: false}, errors.New("prepare failed")
}

func TestRunnerOrderAndSuccess(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, cleanup := createFlowTask(t, db, wsID)
	t.Cleanup(cleanup)
	run, _, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	runner := &flow.FlowRunner{DB: db, Nodes: flow.DefaultExecutors()}

	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	nodes, _ := mysqlstore.ListFlowNodeRuns(ctx, db, run.ID)
	if nodes[0].Status != domain.FlowNodeCompleted {
		t.Fatalf("env = %q", nodes[0].Status)
	}
	if nodes[1].Status != domain.FlowNodePending {
		t.Fatalf("agent_prompt = %q（应由机器领取）", nodes[1].Status)
	}
	got, _ := mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunRunning {
		t.Fatalf("run = %q", got.Status)
	}

	// 机器完成 agent_prompt 后，RunOnce 执行 mars_acceptance
	if _, err := mysqlstore.UpdateFlowNodeStatus(ctx, db, nodes[1].ID,
		domain.FlowNodePending, domain.FlowNodeCompleted, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatalf("RunOnce mars: %v", err)
	}
	got, _ = mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunSucceeded {
		t.Fatalf("run = %q", got.Status)
	}
}

func TestRunnerFailure(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, cleanup := createFlowTask(t, db, wsID)
	t.Cleanup(cleanup)
	run, _, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	runner := &flow.FlowRunner{DB: db, Nodes: map[flow.NodeKey]flow.Node{
		flow.NodeEnvironmentPrepare: failingNode{},
	}}
	if err := runner.RunOnce(ctx, run.ID); err == nil {
		t.Fatal("节点失败应返回错误")
	}
	nodes, _ := mysqlstore.ListFlowNodeRuns(ctx, db, run.ID)
	if nodes[0].Status != domain.FlowNodeFailed {
		t.Fatalf("env = %q", nodes[0].Status)
	}
	got, _ := mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunFailed {
		t.Fatalf("run = %q", got.Status)
	}
}

func TestNodeClaim(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, cleanup := createFlowTask(t, db, wsID)
	t.Cleanup(cleanup)
	run, _, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	runner := &flow.FlowRunner{DB: db, Nodes: flow.DefaultExecutors()}
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	node, err := flow.Claim(ctx, db, run.ID, "m1", time.Minute)
	if err != nil || node.LeaseEpoch != 1 || node.NodeKey != string(flow.NodeAgentPrompt) {
		t.Fatalf("Claim m1 = %+v, err=%v", node, err)
	}
	if _, err := flow.Claim(ctx, db, run.ID, "m2", time.Minute); !errors.Is(err, flow.ErrNodeLeased) {
		t.Fatalf("重复领取应被拒绝，实际: %v", err)
	}
	if err := flow.Renew(ctx, db, node.ID, "m1", 2, time.Minute); !errors.Is(err, flow.ErrLeaseLost) {
		t.Fatalf("旧 epoch 续约应失败，实际: %v", err)
	}
	if err := flow.Renew(ctx, db, node.ID, "m1", 1, time.Minute); err != nil {
		t.Fatalf("续约: %v", err)
	}

	// 租约过期 → 可被重新领取，epoch 递增
	if _, err := db.Exec(`UPDATE flow_node_runs SET lease_expires_at = UTC_TIMESTAMP(3) - INTERVAL 1 SECOND WHERE id = ?`, node.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := flow.Claim(ctx, db, run.ID, "m2", time.Minute)
	if err != nil || reclaimed.LeaseEpoch != 2 || reclaimed.LeaseOwner != "m2" {
		t.Fatalf("重新领取 = %+v, err=%v", reclaimed, err)
	}
	if _, err := flow.Accept(ctx, db, flow.NodeResult{
		NodeID: node.ID, MachineID: "m1", Epoch: 1, Success: true,
	}); !errors.Is(err, flow.ErrLeaseLost) {
		t.Fatalf("旧 lease 完成上报应被拒绝，实际: %v", err)
	}
	if _, err := flow.Accept(ctx, db, flow.NodeResult{
		NodeID: node.ID, MachineID: "m2", Epoch: 2, Success: true,
		Payload: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunSucceeded {
		t.Fatalf("run = %q", got.Status)
	}
}

func TestRerunInvalidatesDownstream(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, cleanup := createFlowTask(t, db, wsID)
	t.Cleanup(cleanup)
	run, nodes, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	runner := &flow.FlowRunner{DB: db, Nodes: flow.DefaultExecutors()}
	if err := runner.RunOnce(ctx, run.ID); err != nil { // env 完成
		t.Fatal(err)
	}
	if _, err := flow.Claim(ctx, db, run.ID, "m1", time.Minute); err != nil {
		t.Fatal(err)
	}
	claimed, err := mysqlstore.GetFlowNodeRun(ctx, db, nodes[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Accept(ctx, db, flow.NodeResult{
		NodeID: claimed.ID, MachineID: "m1", Epoch: claimed.LeaseEpoch, Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(ctx, run.ID); err != nil { // mars 完成
		t.Fatal(err)
	}
	got, _ := mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunSucceeded {
		t.Fatalf("准备阶段 run = %q", got.Status)
	}

	// 让 env 失败并重跑：下游 agent/mars 必须 stale
	if _, err := db.Exec(`UPDATE flow_node_runs SET status = 'failed' WHERE id = ?`, nodes[0].ID); err != nil {
		t.Fatal(err)
	}
	rerun, err := flow.Rerun(ctx, db, nodes[0].ID)
	if err != nil || rerun.Status != domain.FlowNodePending || rerun.AttemptNo != 2 {
		t.Fatalf("Rerun = %+v, err=%v", rerun, err)
	}
	after, _ := mysqlstore.ListFlowNodeRuns(ctx, db, run.ID)
	if after[1].Status != domain.FlowNodeStale || after[2].Status != domain.FlowNodeStale {
		t.Fatalf("下游状态 = %q/%q", after[1].Status, after[2].Status)
	}
	got, _ = mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunRunning {
		t.Fatalf("重跑后 run = %q", got.Status)
	}

	// 重新执行：env → 领取 agent → mars，最终成功
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := flow.Claim(ctx, db, run.ID, "m2", time.Minute)
	if err != nil {
		t.Fatalf("重跑后领取: %v", err)
	}
	if _, err := flow.Accept(ctx, db, flow.NodeResult{
		NodeID: reclaimed.ID, MachineID: "m2", Epoch: reclaimed.LeaseEpoch, Success: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = mysqlstore.GetFlowRun(ctx, db, run.ID)
	if got.Status != domain.FlowRunSucceeded {
		t.Fatalf("重算后 run = %q", got.Status)
	}
}

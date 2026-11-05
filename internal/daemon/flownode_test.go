package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/transport/acp"
)

type fakeFlowClient struct {
	claim      *FlowNodeClaim
	completed  []json.RawMessage
	failed     []string
	heartbeats int64
	leaseLost  bool
}

func (f *fakeFlowClient) ClaimFlowNode(context.Context) (*FlowNodeClaim, bool, error) {
	if f.claim == nil {
		return nil, false, nil
	}
	claim := f.claim
	f.claim = nil
	return claim, true, nil
}

func (f *fakeFlowClient) HeartbeatFlowNode(context.Context, string, uint64) error {
	atomic.AddInt64(&f.heartbeats, 1)
	if f.leaseLost {
		return ErrFlowLeaseLost
	}
	return nil
}

func (f *fakeFlowClient) CompleteFlowNode(_ context.Context, _ string, _ uint64, payload json.RawMessage) error {
	f.completed = append(f.completed, payload)
	return nil
}

func (f *fakeFlowClient) FailFlowNode(_ context.Context, _ string, _ uint64, reason string) error {
	f.failed = append(f.failed, reason)
	return nil
}

func (f *fakeFlowClient) AckFlowNodeCancel(context.Context, string) error { return nil }

func flowClaim() *FlowNodeClaim {
	snapshot, _ := json.Marshal(map[string]any{
		"nodes": []map[string]any{
			{"key": "environment_prepare"},
			{"key": "agent_prompt", "config": map[string]any{"prompt": "验收提示"}},
			{"key": "mars_acceptance"},
		},
	})
	return &FlowNodeClaim{
		NodeID: "node-1", RunID: "run-1", TaskID: "task-1", NodeKey: "agent_prompt",
		Ordinal: 2, LeaseEpoch: 3, AttemptNo: 1,
		Inputs: json.RawMessage(`{}`), TemplateSnapshot: snapshot,
	}
}

func TestFlowNode(t *testing.T) {
	client := &fakeFlowClient{claim: flowClaim()}
	worker := &FlowNodeWorker{
		Client: client,
		ProviderConfig: acp.Config{
			Bin: fakeAgent(t), ExpectedVersion: "0.100.0", Cwd: t.TempDir(),
			InitializeTimeout: 2 * time.Second, InputTimeout: 5 * time.Second,
		},
		Spawner: acp.DefaultSpawner{}, Heartbeat: 10 * time.Millisecond,
	}
	done, err := worker.RunOnce(context.Background())
	if err != nil || !done {
		t.Fatalf("RunOnce: done=%v, err=%v", done, err)
	}
	if len(client.completed) != 1 || len(client.failed) != 0 {
		t.Fatalf("completed=%d failed=%d", len(client.completed), len(client.failed))
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(client.completed[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Summary != "hello" {
		t.Fatalf("summary = %q", payload.Summary)
	}
}

func TestFlowNodeFailure(t *testing.T) {
	client := &fakeFlowClient{claim: flowClaim()}
	worker := &FlowNodeWorker{
		Client: client,
		ProviderConfig: acp.Config{
			Bin: t.TempDir() + "/missing-agent", ExpectedVersion: "0.100.0",
			Cwd: t.TempDir(), InitializeTimeout: 500 * time.Millisecond,
		},
		Spawner: acp.DefaultSpawner{},
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("启动失败应返回错误")
	}
	if len(client.failed) != 1 || len(client.completed) != 0 {
		t.Fatalf("failed=%d completed=%d", len(client.failed), len(client.completed))
	}
}

func TestFlowNodeLeaseLost(t *testing.T) {
	client := &fakeFlowClient{leaseLost: true}
	worker := &FlowNodeWorker{Client: client, Heartbeat: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	var lost atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		worker.heartbeatLoop(ctx, flowClaim(), &lost, cancel)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !lost.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !lost.Load() {
		t.Fatal("租约失效未触发取消")
	}
	<-heartbeatDone
	if ctx.Err() == nil {
		t.Fatal("lease lost 应取消执行上下文")
	}
	_ = errors.Is(ErrFlowLeaseLost, ErrFlowLeaseLost)
}

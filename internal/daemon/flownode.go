package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/roarzx/my-aladin/internal/agentevent"
	"github.com/roarzx/my-aladin/internal/transport/acp"
)

// FlowNodeClient 是 Flow 节点领取/续约/上报的最小客户端（CLIClient 实现）。
type FlowNodeClient interface {
	ClaimFlowNode(ctx context.Context) (*FlowNodeClaim, bool, error)
	HeartbeatFlowNode(ctx context.Context, nodeID string, epoch uint64) error
	CompleteFlowNode(ctx context.Context, nodeID string, epoch uint64, payload json.RawMessage) error
	FailFlowNode(ctx context.Context, nodeID string, epoch uint64, reason string) error
	AckFlowNodeCancel(ctx context.Context, nodeID string) error
}

// FlowNodeWorker 领取并执行 agent_prompt 节点（C11）。
type FlowNodeWorker struct {
	Client         FlowNodeClient
	ProviderConfig acp.Config
	Spawner        acp.Spawner
	Interval       time.Duration
	Heartbeat      time.Duration
	Log            *slog.Logger
}

// Run 周期领取节点，直到 ctx 取消。
func (w *FlowNodeWorker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil {
				w.log().Warn("Flow 节点执行失败", "err", err)
			}
		}
	}
}

// RunOnce 领取并执行至多一个节点；没有可领取节点返回 false。
func (w *FlowNodeWorker) RunOnce(ctx context.Context) (bool, error) {
	claim, ok, err := w.Client.ClaimFlowNode(ctx)
	if err != nil {
		return false, err
	}
	if !ok || claim == nil {
		return false, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var leaseLost atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		w.heartbeatLoop(runCtx, claim, &leaseLost, cancel)
	}()

	text, err := w.execute(runCtx, claim)
	cancel()
	<-heartbeatDone

	if leaseLost.Load() {
		_ = w.Client.AckFlowNodeCancel(context.Background(), claim.NodeID)
		return true, nil
	}
	if err != nil {
		_ = w.Client.FailFlowNode(context.Background(), claim.NodeID, claim.LeaseEpoch, err.Error())
		return true, err
	}
	payload, _ := json.Marshal(map[string]any{"summary": text})
	if err := w.Client.CompleteFlowNode(context.Background(), claim.NodeID, claim.LeaseEpoch, payload); err != nil {
		return true, fmt.Errorf("上报节点结果: %w", err)
	}
	return true, nil
}

func (w *FlowNodeWorker) heartbeatLoop(ctx context.Context, claim *FlowNodeClaim,
	leaseLost *atomic.Bool, cancel context.CancelFunc) {
	interval := w.Heartbeat
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Client.HeartbeatFlowNode(ctx, claim.NodeID, claim.LeaseEpoch); err != nil {
				if errors.Is(err, ErrFlowLeaseLost) {
					leaseLost.Store(true)
					cancel()
					return
				}
				w.log().Warn("Flow 节点续约失败", "node", claim.NodeID, "err", err)
			}
		}
	}
}

func (w *FlowNodeWorker) execute(ctx context.Context, claim *FlowNodeClaim) (string, error) {
	provider := acp.NewProvider(w.ProviderConfig, w.Spawner, w.log())
	session, err := provider.Start(ctx)
	if err != nil {
		return "", fmt.Errorf("启动 Provider: %w", err)
	}
	defer func() { _ = session.Close() }()

	var builder strings.Builder
	result, err := session.Prompt(ctx, w.prompt(claim), func(event agentevent.Event) {
		if event.Kind == agentevent.KindStreamDelta {
			builder.WriteString(event.Text)
		}
	})
	if err != nil {
		return "", err
	}
	if result.StopReason != "end_turn" {
		return "", fmt.Errorf("节点以 %q 结束", result.StopReason)
	}
	return builder.String(), nil
}

// prompt 从冻结模板的 agent_prompt 配置取提示词，其次 inputs.prompt。
func (w *FlowNodeWorker) prompt(claim *FlowNodeClaim) string {
	var snapshot struct {
		Nodes []struct {
			Key    string          `json:"key"`
			Config json.RawMessage `json:"config"`
		} `json:"nodes"`
	}
	_ = json.Unmarshal(claim.TemplateSnapshot, &snapshot)
	for _, node := range snapshot.Nodes {
		if node.Key != "agent_prompt" {
			continue
		}
		var config struct {
			Prompt string `json:"prompt"`
		}
		if json.Unmarshal(node.Config, &config) == nil && config.Prompt != "" {
			return config.Prompt
		}
	}
	var inputs struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal(claim.Inputs, &inputs) == nil && inputs.Prompt != "" {
		return inputs.Prompt
	}
	return "执行验收节点"
}

func (w *FlowNodeWorker) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

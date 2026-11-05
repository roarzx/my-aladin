package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/runtime"
	"github.com/roarzx/my-aladin/internal/session"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// ErrMachineOffline 表示目标机器没有活跃连接。
var ErrMachineOffline = errors.New("hub: 机器不在线")

// Hub 维护 machineID → 会话，并实现控制面的 Runtime 与 session.Dispatcher。
type Hub struct {
	mu           sync.Mutex
	sessions     map[string]*Session
	execMachines map[string]string

	log *slog.Logger
	// Lease 用于心跳续约（E4）；为空时忽略心跳中的 active_executions。
	Lease *orchestration.Lease
	// OnEvent 处理 daemon 上行的非响应消息（step_done / step_fail 等）。
	OnEvent func(ctx context.Context, machineID string, env protocol.Envelope) error
}

// NewHub 构造连接注册表。
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		sessions:     make(map[string]*Session),
		execMachines: make(map[string]string),
		log:          logger,
	}
}

// Handler 供 ws.Server 在握手后调用；阻塞直到连接结束。
func (h *Hub) Handler(ctx context.Context, machineID string, conn *Conn) error {
	return h.serve(ctx, machineID, conn)
}

func (h *Hub) serve(ctx context.Context, machineID string, conn EnvelopeConn) error {
	wsSession := NewSession(conn)
	wsSession.OnEnvelope(func(env protocol.Envelope) {
		if env.Type == protocol.TypeHeartbeat {
			h.handleHeartbeat(ctx, env)
		}
		if h.OnEvent != nil {
			if err := h.OnEvent(ctx, machineID, env); err != nil {
				h.log.Warn("处理事件失败", "machine", machineID, "err", err)
			}
		}
	})

	h.mu.Lock()
	h.sessions[machineID] = wsSession
	h.mu.Unlock()
	h.log.Info("daemon 会话已注册", "machine", machineID)

	defer func() {
		h.mu.Lock()
		if h.sessions[machineID] == wsSession {
			delete(h.sessions, machineID)
		}
		h.mu.Unlock()
		_ = wsSession.Close()
		h.log.Info("daemon 会话已注销", "machine", machineID)
	}()

	return wsSession.Run()
}

// Dispatch 实现 runtime.Runtime：向目标机器下发 start_step_session。
func (h *Hub) Dispatch(ctx context.Context, req runtime.DispatchRequest) (string, error) {
	if req.MachineID == "" {
		return "", errors.New("hub: DispatchRequest.MachineID 必填")
	}
	wsSession, err := h.session(req.MachineID)
	if err != nil {
		return "", err
	}
	if req.ExecutionID == "" {
		return "", errors.New("hub: execution_id 必填")
	}
	if req.AttemptNo == 0 {
		return "", errors.New("hub: attempt_no 必填")
	}

	params, err := json.Marshal(map[string]any{
		"execution_id": req.ExecutionID,
		"task_id":      req.TaskID,
		"step_id":      req.StepID,
		"attempt_no":   req.AttemptNo,
		"lease_epoch":  req.LeaseEpoch,
		"branch":       req.Branch,
		"prompt":       req.Prompt,
	})
	if err != nil {
		return "", err
	}
	attempt := req.AttemptNo
	envelope := protocol.Envelope{
		Type:             protocol.TypeCommand,
		ID:               idgen.New(),
		TraceID:          idgen.New(),
		ExecutionID:      req.ExecutionID,
		ExecutionAttempt: &attempt,
		StepID:           req.StepID,
		Payload: mustJSON(protocol.CommandPayload{
			Method: "start_step_session",
			Params: params,
		}),
	}

	response, err := wsSession.Dispatch(ctx, envelope)
	if err != nil {
		return "", err
	}
	payload, err := decodeResponse(response)
	if err != nil {
		return "", err
	}
	if !payload.Accepted {
		return "", errcode.Newf(errcode.RuntimeStartFailed, "daemon 拒绝执行: %s", errorMessage(payload))
	}

	h.mu.Lock()
	h.execMachines[req.ExecutionID] = req.MachineID
	h.mu.Unlock()
	return req.ExecutionID, nil
}

// heartbeatData 是 daemon 心跳中与租约相关的字段（9.7）。
type heartbeatData struct {
	ActiveExecutions []struct {
		ExecutionID string `json:"execution_id"`
		LeaseEpoch  uint64 `json:"lease_epoch"`
	} `json:"active_executions"`
}

// handleHeartbeat 按 execution_id 续约；epoch 不等于当前代次则拒绝（旧心跳不得恢复资格）。
func (h *Hub) handleHeartbeat(ctx context.Context, env protocol.Envelope) {
	if h.Lease == nil || len(env.Payload) == 0 {
		return
	}
	var payload heartbeatData
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return
	}
	for _, active := range payload.ActiveExecutions {
		if err := h.Lease.RenewByExecution(ctx, active.ExecutionID, active.LeaseEpoch); err != nil {
			h.log.Debug("续约被拒绝", "execution", active.ExecutionID,
				"epoch", active.LeaseEpoch, "err", err)
		}
	}
}

// Inspect 暂不维护执行快照（由 daemon 侧负责），返回 unknown。
func (h *Hub) Inspect(context.Context, string) (runtime.ExecutionSnapshot, error) {
	return runtime.ExecutionSnapshot{State: runtime.StateUnknown}, nil
}

// Cancel 向执行所在机器发送 stop_session。
func (h *Hub) Cancel(ctx context.Context, executionID string) (bool, error) {
	h.mu.Lock()
	machineID := h.execMachines[executionID]
	h.mu.Unlock()
	if machineID == "" {
		return false, nil
	}
	wsSession, err := h.session(machineID)
	if err != nil {
		return false, err
	}
	params, _ := json.Marshal(map[string]any{"execution_id": executionID, "reason": "cancelled"})
	response, err := wsSession.Dispatch(ctx, protocol.Envelope{
		Type: protocol.TypeCommand, ID: idgen.New(), TraceID: idgen.New(),
		Payload: mustJSON(protocol.CommandPayload{Method: "stop_session", Params: params}),
	})
	if err != nil {
		return false, err
	}
	payload, err := decodeResponse(response)
	if err != nil {
		return false, err
	}
	return payload.Accepted, nil
}

// Capabilities 返回控制面视角的能力（按机器查询由 MachineAdapter 负责）。
func (h *Hub) Capabilities() runtime.CapabilitySet { return runtime.CapabilitySet{} }

// DispatchPlanning 实现 session.Dispatcher：下发 start_planning_session。
func (h *Hub) DispatchPlanning(ctx context.Context, req session.PlanningDispatch) error {
	wsSession, err := h.session(req.MachineID)
	if err != nil {
		return err
	}
	params, err := json.Marshal(map[string]any{
		"task_id":    req.TaskID,
		"agent_id":   req.AgentID,
		"session_id": req.SessionID,
		"prompt":     req.Prompt,
	})
	if err != nil {
		return err
	}
	response, err := wsSession.Dispatch(ctx, protocol.Envelope{
		Type:      protocol.TypeCommand,
		ID:        idgen.New(),
		TraceID:   idgen.New(),
		SessionID: req.SessionID,
		Payload: mustJSON(protocol.CommandPayload{
			Method: "start_planning_session",
			Params: params,
		}),
	})
	if err != nil {
		return err
	}
	payload, err := decodeResponse(response)
	if err != nil {
		return err
	}
	if !payload.Accepted {
		return errcode.Newf(errcode.RuntimeStartFailed, "daemon 拒绝规划会话: %s", errorMessage(payload))
	}
	return nil
}

func (h *Hub) session(machineID string) (*Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	wsSession := h.sessions[machineID]
	if wsSession == nil {
		return nil, fmt.Errorf("%w: %s", ErrMachineOffline, machineID)
	}
	return wsSession, nil
}

func decodeResponse(env protocol.Envelope) (protocol.ResponsePayload, error) {
	var payload protocol.ResponsePayload
	if len(env.Payload) == 0 {
		return payload, errors.New("hub: 响应为空")
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return payload, fmt.Errorf("hub: 解析响应: %w", err)
	}
	return payload, nil
}

func errorMessage(payload protocol.ResponsePayload) string {
	if payload.Error == nil {
		return "unspecified"
	}
	return payload.Error.Code + ": " + payload.Error.Message
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic("ws: marshal 失败: " + err.Error())
	}
	return data
}

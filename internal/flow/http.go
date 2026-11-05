package flow

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Handler 提供机器侧 Flow 节点领取 API（G4 的服务端，C11 使用）。
type Handler struct {
	DB  *sql.DB
	TTL time.Duration
}

type nodeClaimResponse struct {
	Node struct {
		ID         string `json:"id"`
		RunID      string `json:"runId"`
		NodeKey    string `json:"nodeKey"`
		Ordinal    int    `json:"ordinal"`
		LeaseEpoch uint64 `json:"leaseEpoch"`
		AttemptNo  int    `json:"attemptNo"`
	} `json:"node"`
	Run struct {
		ID               string          `json:"id"`
		TaskID           string          `json:"taskId"`
		TemplateSnapshot json.RawMessage `json:"templateSnapshot"`
		Inputs           json.RawMessage `json:"inputs"`
	} `json:"run"`
}

// Claim 处理 POST /api/v1/cli/flow-run-nodes/claim。
func (h *Handler) Claim(w http.ResponseWriter, r *http.Request) {
	machineID := httpapi.MachineIDFrom(r.Context())
	ttl := h.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	node, run, err := ClaimNext(r.Context(), h.DB, machineID, ttl)
	if errors.Is(err, ErrNoClaimable) {
		httpapi.JSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.Newf(errcode.UpstreamUnavailable, "领取节点失败: %v", err))
		return
	}
	var out nodeClaimResponse
	out.Node.ID = node.ID
	out.Node.RunID = node.RunID
	out.Node.NodeKey = node.NodeKey
	out.Node.Ordinal = node.Ordinal
	out.Node.LeaseEpoch = node.LeaseEpoch
	out.Node.AttemptNo = node.AttemptNo
	out.Run.ID = run.ID
	out.Run.TaskID = run.TaskID
	out.Run.TemplateSnapshot = run.TemplateSnapshot
	out.Run.Inputs = run.Inputs
	httpapi.Data(w, http.StatusOK, out)
}

// Heartbeat 处理 POST /flow-run-nodes/{id}/heartbeat。
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LeaseEpoch uint64 `json:"leaseEpoch"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	machineID := httpapi.MachineIDFrom(r.Context())
	err := Renew(r.Context(), h.DB, chi.URLParam(r, "id"), machineID, body.LeaseEpoch, h.ttl())
	if errors.Is(err, ErrLeaseLost) {
		httpapi.Fail(w, errcode.New(errcode.Conflict, "节点租约已失效"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "续约失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, map[string]any{"renewed": true})
}

// Complete 处理 POST /flow-run-nodes/{id}/complete。
func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	h.accept(w, r, true)
}

// Fail 处理 POST /flow-run-nodes/{id}/fail。
func (h *Handler) Fail(w http.ResponseWriter, r *http.Request) {
	h.accept(w, r, false)
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request, success bool) {
	var body struct {
		LeaseEpoch uint64          `json:"leaseEpoch"`
		Payload    json.RawMessage `json:"payload,omitempty"`
		Reason     string          `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "请求体不合法"))
		return
	}
	payload := body.Payload
	if !success && len(payload) == 0 {
		payload, _ = json.Marshal(map[string]any{"error": body.Reason})
	}
	node, err := Accept(r.Context(), h.DB, NodeResult{
		NodeID: chi.URLParam(r, "id"), MachineID: httpapi.MachineIDFrom(r.Context()),
		Epoch: body.LeaseEpoch, Success: success, Payload: payload,
	})
	if errors.Is(err, ErrLeaseLost) {
		httpapi.Fail(w, errcode.New(errcode.Conflict, "节点租约已失效"))
		return
	}
	if err != nil {
		httpapi.Fail(w, err)
		return
	}
	httpapi.Data(w, http.StatusOK, map[string]any{"status": string(node.Status)})
}

// CancelAck 处理 POST /flow-run-nodes/{id}/cancel-ack：取消的机器确认只记审计。
func (h *Handler) CancelAck(w http.ResponseWriter, r *http.Request) {
	httpapi.Data(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (h *Handler) ttl() time.Duration {
	if h.TTL > 0 {
		return h.TTL
	}
	return 30 * time.Second
}

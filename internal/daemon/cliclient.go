package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/roarzx/my-aladin/internal/agentctx"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ErrContextConflict 表示服务端上下文版本冲突（HTTP 409）。
//
// 与 agentctx.ErrVersionConflict 是同一哨兵：上下文仲裁统一走 agentctx（E1）。
var ErrContextConflict = agentctx.ErrVersionConflict

// ErrFlowLeaseLost 表示 Flow 节点租约已失效（HTTP 409）。
var ErrFlowLeaseLost = errors.New("flow: 节点租约已失效")

// statusError 保留 HTTP 状态码，同时保持与旧哨兵的 errors.Is 兼容。
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

func statusOf(err error, target int) bool {
	var se *statusError
	return errors.As(err, &se) && se.status == target
}

// CLIClient 是 daemon → 控制面的 CLI HTTP API 客户端（9.6.1）。
type CLIClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewCLIClient 构造客户端。
func NewCLIClient(baseURL, token string) *CLIClient {
	return &CLIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ContextVersion 是版本列表项。
type ContextVersion struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

// ListContexts 列出某 scope 下的上下文版本。
func (c *CLIClient) ListContexts(ctx context.Context, taskID, scope, scopeID string) ([]ContextVersion, error) {
	var out []ContextVersion
	path := c.contextPath(taskID, scope, "files") + "?scopeId=" + url.QueryEscape(scopeID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetContext 读取指定版本内容。
func (c *CLIClient) GetContext(ctx context.Context, taskID, scope, scopeID, kind string, version int) (string, error) {
	var out struct {
		Content string `json:"content"`
	}
	path := c.contextPath(taskID, scope, "files/"+url.PathEscape(kind)+"/"+strconv.Itoa(version)) +
		"?scopeId=" + url.QueryEscape(scopeID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	return out.Content, nil
}

// GetLatest 返回某 kind 的最新内容与版本；不存在时版本为 0。
func (c *CLIClient) GetLatest(ctx context.Context, taskID, scope, scopeID, kind string) (string, int, error) {
	versions, err := c.ListContexts(ctx, taskID, scope, scopeID)
	if err != nil {
		return "", 0, err
	}
	latest := 0
	for _, v := range versions {
		if v.Kind == kind && v.Version > latest {
			latest = v.Version
		}
	}
	if latest == 0 {
		return "", 0, nil
	}
	content, err := c.GetContext(ctx, taskID, scope, scopeID, kind, latest)
	if err != nil {
		return "", 0, err
	}
	return content, latest, nil
}

// PutContext 以 expectedVersion 做 CAS 写入；冲突返回 ErrContextConflict。
func (c *CLIClient) PutContext(ctx context.Context, taskID, scope, scopeID, kind, content string, expectedVersion int) (int, error) {
	body := map[string]any{
		"scopeId":         scopeID,
		"content":         content,
		"expectedVersion": expectedVersion,
	}
	var out struct {
		Version int `json:"version"`
	}
	path := c.contextPath(taskID, scope, "files/"+url.PathEscape(kind))
	if err := c.do(ctx, http.MethodPut, path, body, &out); err != nil {
		return 0, err
	}
	return out.Version, nil
}

func (c *CLIClient) contextPath(taskID, scope, suffix string) string {
	return "/api/v1/cli/contexts/" + url.PathEscape(taskID) + "/" + url.PathEscape(scope) + "/" + suffix
}

func (c *CLIClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("调用 CLI API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode == http.StatusConflict {
		return &statusError{status: http.StatusConflict, err: ErrContextConflict}
	}
	if resp.StatusCode >= 400 {
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &body)
		code := errcode.Code(body.Error.Code)
		if !code.Valid() {
			code = errcode.UpstreamUnavailable
		}
		return errcode.Newf(code, "CLI API: %s", body.Error.Message)
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, out)
}

// FlowNodeClaim 是机器领取到的 Flow 节点。
type FlowNodeClaim struct {
	NodeID           string          `json:"nodeId"`
	RunID            string          `json:"runId"`
	TaskID           string          `json:"taskId"`
	NodeKey          string          `json:"nodeKey"`
	Ordinal          int             `json:"ordinal"`
	LeaseEpoch       uint64          `json:"leaseEpoch"`
	AttemptNo        int             `json:"attemptNo"`
	Inputs           json.RawMessage `json:"inputs"`
	TemplateSnapshot json.RawMessage `json:"templateSnapshot"`
}

// ClaimFlowNode 领取一个 agent_prompt 节点；没有可领取节点时返回 ok=false。
func (c *CLIClient) ClaimFlowNode(ctx context.Context) (*FlowNodeClaim, bool, error) {
	var out *struct {
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
	if err := c.do(ctx, http.MethodPost, "/api/v1/cli/flow-run-nodes/claim", map[string]any{}, &out); err != nil {
		return nil, false, err
	}
	if out == nil {
		return nil, false, nil
	}
	return &FlowNodeClaim{
		NodeID: out.Node.ID, RunID: out.Node.RunID, TaskID: out.Run.TaskID,
		NodeKey: out.Node.NodeKey, Ordinal: out.Node.Ordinal,
		LeaseEpoch: out.Node.LeaseEpoch, AttemptNo: out.Node.AttemptNo,
		Inputs: out.Run.Inputs, TemplateSnapshot: out.Run.TemplateSnapshot,
	}, true, nil
}

// HeartbeatFlowNode 续约。
func (c *CLIClient) HeartbeatFlowNode(ctx context.Context, nodeID string, epoch uint64) error {
	err := c.do(ctx, http.MethodPost,
		"/api/v1/cli/flow-run-nodes/"+url.PathEscape(nodeID)+"/heartbeat",
		map[string]any{"leaseEpoch": epoch}, nil)
	if statusOf(err, http.StatusConflict) {
		return ErrFlowLeaseLost
	}
	return err
}

// CompleteFlowNode 上报成功结果。
func (c *CLIClient) CompleteFlowNode(ctx context.Context, nodeID string, epoch uint64, payload json.RawMessage) error {
	err := c.do(ctx, http.MethodPost,
		"/api/v1/cli/flow-run-nodes/"+url.PathEscape(nodeID)+"/complete",
		map[string]any{"leaseEpoch": epoch, "payload": payload}, nil)
	if statusOf(err, http.StatusConflict) {
		return ErrFlowLeaseLost
	}
	return err
}

// FailFlowNode 上报失败原因。
func (c *CLIClient) FailFlowNode(ctx context.Context, nodeID string, epoch uint64, reason string) error {
	err := c.do(ctx, http.MethodPost,
		"/api/v1/cli/flow-run-nodes/"+url.PathEscape(nodeID)+"/fail",
		map[string]any{"leaseEpoch": epoch, "reason": reason}, nil)
	if statusOf(err, http.StatusConflict) {
		return ErrFlowLeaseLost
	}
	return err
}

// AckFlowNodeCancel 向服务端确认取消已处理（审计用）。
func (c *CLIClient) AckFlowNodeCancel(ctx context.Context, nodeID string) error {
	return c.do(ctx, http.MethodPost,
		"/api/v1/cli/flow-run-nodes/"+url.PathEscape(nodeID)+"/cancel-ack", nil, nil)
}

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
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
		return ErrContextConflict
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

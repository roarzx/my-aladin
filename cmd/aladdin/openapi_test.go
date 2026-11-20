package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newOpenAPIServer 起一个假的控制面，记录最近一次请求。
type recordedRequest struct {
	method string
	path   string
	token  string
	body   map[string]any
}

func newOpenAPIServer(t *testing.T, status int, response string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.RequestURI()
		rec.token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&rec.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, rec
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := stdout
	buf := &bytes.Buffer{}
	stdout = buf
	t.Cleanup(func() { stdout = original })
	err := fn()
	return buf.String(), err
}

func TestIssueGetCallsCLIEndpoint(t *testing.T) {
	server, rec := newOpenAPIServer(t, http.StatusOK,
		`{"data":{"id":"issue-1","name":"修登录","status":"executing","mode":"ordinary","version":2,"updatedAt":"2026-11-20T03:00:00Z"}}`)

	out, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"get", "--api-url", server.URL,
			"--token", "tok", "--id", "issue-1", "--output", "text"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/v1/cli/issues/issue-1" {
		t.Fatalf("请求 = %s %s", rec.method, rec.path)
	}
	if rec.token != "tok" {
		t.Fatalf("令牌 = %q", rec.token)
	}
	if !strings.Contains(out, "issue-1") || !strings.Contains(out, "executing") {
		t.Fatalf("输出 = %q", out)
	}
}

func TestIssueGetFallsBackToEnvContext(t *testing.T) {
	t.Setenv("ALADDIN_ISSUE_ID", "issue-env")
	server, rec := newOpenAPIServer(t, http.StatusOK, `{"data":{"id":"issue-env"}}`)

	if _, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"get", "--api-url", server.URL, "--token", "tok"})
	}); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/api/v1/cli/issues/issue-env" {
		t.Fatalf("应从 ALADDIN_ISSUE_ID 取 ID，实际 %s", rec.path)
	}
}

func TestIssueUpdateStatusSendsStatusAndReason(t *testing.T) {
	server, rec := newOpenAPIServer(t, http.StatusOK,
		`{"data":{"id":"issue-1","status":"completed"}}`)

	if _, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"update-status", "--api-url", server.URL,
			"--token", "tok", "--id", "issue-1", "--status", "completed", "--reason", "跑通测试"})
	}); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/api/v1/cli/issues/issue-1/status" || rec.method != http.MethodPost {
		t.Fatalf("请求 = %s %s", rec.method, rec.path)
	}
	if rec.body["status"] != "completed" || rec.body["reason"] != "跑通测试" {
		t.Fatalf("请求体 = %v", rec.body)
	}
}

func TestIssueCreateSendsOptionalFields(t *testing.T) {
	server, rec := newOpenAPIServer(t, http.StatusCreated, `{"data":{"id":"issue-9","status":"pending"}}`)

	if _, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"create", "--api-url", server.URL, "--token", "tok",
			"--name", "加导出", "--agent-id", "agent-1",
			"--requirement-content", "导出 CSV", "--idempotency-key", "k1"})
	}); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/api/v1/cli/issues" {
		t.Fatalf("路径 = %s", rec.path)
	}
	for key, want := range map[string]any{
		"name": "加导出", "agentId": "agent-1",
		"requirementContent": "导出 CSV", "idempotencyKey": "k1",
	} {
		if rec.body[key] != want {
			t.Fatalf("%s = %v，期望 %v", key, rec.body[key], want)
		}
	}
	if _, ok := rec.body["repositoryUrl"]; ok {
		t.Fatal("未提供仓库时不应发送 repositoryUrl")
	}
}

func TestIssueListBuildsQuery(t *testing.T) {
	server, rec := newOpenAPIServer(t, http.StatusOK, `{"data":[]}`)

	if _, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"list", "--api-url", server.URL, "--token", "tok",
			"--keyword", "登录", "--status", "in_progress", "--created-at-start", "2026-11-01"})
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"keyword=%E7%99%BB%E5%BD%95", "status=in_progress", "created-at-start=2026-11-01"} {
		if !strings.Contains(rec.path, want) {
			t.Fatalf("查询串缺少 %s: %s", want, rec.path)
		}
	}
}

func TestIssueCommandSurfacesAPIError(t *testing.T) {
	server, _ := newOpenAPIServer(t, http.StatusForbidden,
		`{"error":{"code":"FORBIDDEN","message":"本机没有该 Issue 的 Agent，不能上报"}}`)

	_, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"update-status", "--api-url", server.URL,
			"--token", "tok", "--id", "issue-1", "--status", "completed"})
	})
	if err == nil || !strings.Contains(err.Error(), "本机没有该 Issue 的 Agent") {
		t.Fatalf("应带出服务端错误，实际 %v", err)
	}
}

func TestIssueRequiresToken(t *testing.T) {
	t.Setenv("ALADDIN_TOKEN", "")
	err := runOpenAPIIssue([]string{"get", "--id", "issue-1"})
	if err == nil || !strings.Contains(err.Error(), "缺少机器令牌") {
		t.Fatalf("err = %v", err)
	}
}

func TestIssueInputRequest(t *testing.T) {
	server, rec := newOpenAPIServer(t, http.StatusCreated,
		`{"data":{"id":"evt-1","issueId":"issue-1","status":"pending"}}`)

	if _, err := captureStdout(t, func() error {
		return runOpenAPIIssue([]string{"input", "request", "--api-url", server.URL,
			"--token", "tok", "--id", "issue-1", "--message", "用哪个分支？",
			"--option", "main", "--option", "dev"})
	}); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/api/v1/cli/issues/issue-1/input-requests" {
		t.Fatalf("路径 = %s", rec.path)
	}
	if rec.body["message"] != "用哪个分支？" {
		t.Fatalf("请求体 = %v", rec.body)
	}
	options, _ := rec.body["options"].([]any)
	if len(options) != 2 {
		t.Fatalf("选项 = %v", rec.body["options"])
	}
}

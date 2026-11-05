package observability_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/roarzx/my-aladin/internal/observability"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := observability.NewLogger(&buf, slog.LevelDebug)
	observability.Fields{
		TaskID: "task-1", StepID: "step-1", SessionID: "sess-1", Attempt: 2,
	}.Logger(logger).Info("hello", "token", "super-secret", "note", "Authorization: Bearer abcdef123456")

	line := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	if !line.Scan() {
		t.Fatal("没有日志输出")
	}
	var record map[string]any
	if err := json.Unmarshal(line.Bytes(), &record); err != nil {
		t.Fatalf("解析 JSON 日志: %v\n%s", err, line.Text())
	}
	if record["task_id"] != "task-1" || record["step_id"] != "step-1" ||
		record["session_id"] != "sess-1" || record["attempt"] != float64(2) {
		t.Fatalf("关联字段 = %+v", record)
	}
	if record["token"] != "***" {
		t.Fatalf("token 未脱敏 = %v", record["token"])
	}
	if strings.Contains(line.Text(), "super-secret") || strings.Contains(line.Text(), "abcdef123456") {
		t.Fatalf("日志含明文凭据: %s", line.Text())
	}
}

func TestTrace(t *testing.T) {
	ctx := context.Background()
	rootCtx, root := observability.StartSpan(ctx, "create-task")
	childCtx, child := observability.StartSpan(rootCtx, "dispatch-step")

	if root.TraceID == "" || child.TraceID != root.TraceID {
		t.Fatalf("trace 不一致: %s / %s", root.TraceID, child.TraceID)
	}
	if child.ParentID != root.SpanID {
		t.Fatalf("parent = %q, want %q", child.ParentID, root.SpanID)
	}
	if observability.TraceID(childCtx) != root.TraceID {
		t.Fatal("TraceID(ctx) 不一致")
	}

	child.SetAttr("attempt", 1)
	if value, ok := child.Attr("attempt"); !ok || value != 1 {
		t.Fatalf("attr = %v", value)
	}
	child.End(errors.New("boom"))
	root.End(nil)
	if !child.Finished() || child.Err == nil || child.Duration() < 0 {
		t.Fatalf("child = %+v", child)
	}
	if root.Err != nil {
		t.Fatal("root 不应有错误")
	}
}

func TestRedact(t *testing.T) {
	input := `Authorization: Bearer abcdef123456 token=topsecret {"password":"p@ss","note":"token=abc"}'`
	cleaned := observability.Redact(input)
	for _, secret := range []string{"abcdef123456", "topsecret", "p@ss"} {
		if strings.Contains(cleaned, secret) {
			t.Fatalf("残留明文 %q: %s", secret, cleaned)
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"kind": "step_fail",
		"data": map[string]any{"reason": "token=abc123", "env": map[string]any{"API_TOKEN": "xyz"}},
	})
	attempt := uint32(1)
	env := protocol.Envelope{
		Type: protocol.TypeEvent, ID: "1", ExecutionID: "e",
		ExecutionAttempt: &attempt, Payload: payload,
	}
	encoded, err := protocol.Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "abc123") || strings.Contains(string(encoded), "xyz") {
		t.Fatalf("事件编码含明文: %s", encoded)
	}
}

func TestMetrics(t *testing.T) {
	metrics := observability.NewMetrics()
	metrics.IncTask("ws-1", "executing")
	metrics.IncTask("ws-1", "chatting")
	metrics.IncFailure("ws-1", "runtime")
	metrics.AddTokens("ws-1", 120)
	metrics.ObserveStepDuration("ws-1", 1500*1e6)
	metrics.IncTask("ws-2", "pending")

	snapshot := metrics.Snapshot()
	if snapshot["ws-1"].Tasks["executing"] != 1 || snapshot["ws-1"].Tasks["chatting"] != 1 {
		t.Fatalf("ws-1 tasks = %+v", snapshot["ws-1"].Tasks)
	}
	if snapshot["ws-1"].FailureByCls["runtime"] != 1 || snapshot["ws-1"].Tokens != 120 {
		t.Fatalf("ws-1 = %+v", snapshot["ws-1"])
	}
	if snapshot["ws-2"].Tasks["pending"] != 1 {
		t.Fatalf("ws-2 = %+v", snapshot["ws-2"])
	}
	// 返回的是拷贝
	snapshot["ws-1"].Tasks["executing"] = 99
	if metrics.Snapshot()["ws-1"].Tasks["executing"] != 1 {
		t.Fatal("快照未隔离")
	}
	rendered := metrics.Render()
	if !strings.Contains(rendered, `workspace="ws-1"`) ||
		!strings.Contains(rendered, "aladdin_failure_total") {
		t.Fatalf("render = %s", rendered)
	}
}

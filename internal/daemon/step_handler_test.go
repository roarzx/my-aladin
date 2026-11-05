package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/transport/acp"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

var (
	fakeAgentOnce sync.Once
	fakeAgentBin  string
	fakeAgentErr  error
)

// fakeAgent 按需构建 fake ACP agent（daemon 包的 TestMain 已被 janitor 用例占用）。
func fakeAgent(t *testing.T) string {
	t.Helper()
	fakeAgentOnce.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(file), "..", "..")
		dir, err := os.MkdirTemp("", "daemon-fake-acp-*")
		if err != nil {
			fakeAgentErr = err
			return
		}
		bin := filepath.Join(dir, "fakeagent")
		build := exec.Command("go", "build", "-o", bin, "./tests/fixtures/acp/fakeagent")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			fakeAgentErr = fmt.Errorf("%v\n%s", err, out)
			return
		}
		fakeAgentBin = bin
	})
	if fakeAgentErr != nil {
		t.Fatalf("构建 fake agent 失败: %v", fakeAgentErr)
	}
	return fakeAgentBin
}

type recordedEvent struct {
	kind        string
	executionID string
	attempt     uint32
	stepID      string
	data        map[string]any
}

type recordedStream struct {
	executionID string
	seq         uint64
	delta       string
	final       bool
}

type recordingSender struct {
	mu      sync.Mutex
	events  []recordedEvent
	streams []recordedStream
	done    chan struct{}
	once    sync.Once
}

func newRecordingSender() *recordingSender {
	return &recordingSender{done: make(chan struct{})}
}

func (s *recordingSender) SendEvent(kind, executionID string, attempt uint32, stepID string, data any) error {
	raw, _ := json.Marshal(data)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	s.mu.Lock()
	s.events = append(s.events, recordedEvent{kind: kind, executionID: executionID, attempt: attempt, stepID: stepID, data: fields})
	s.mu.Unlock()
	if kind == "step_done" || kind == "step_fail" {
		s.once.Do(func() { close(s.done) })
	}
	return nil
}

func (s *recordingSender) SendStream(executionID string, attempt uint32, stepID string, seq uint64, delta string, final bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams = append(s.streams, recordedStream{executionID: executionID, seq: seq, delta: delta, final: final})
	return nil
}

func (s *recordingSender) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(15 * time.Second):
		t.Fatal("等待步骤终态事件超时")
	}
}

func (s *recordingSender) lastEvent() recordedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[len(s.events)-1]
}

func startEnvelope(t *testing.T, params stepParams) protocol.Envelope {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(protocol.CommandPayload{Method: "start_step_session", Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{Type: protocol.TypeCommand, Payload: payload}
}

func newTestStepHandler(t *testing.T, mode string) *StepHandler {
	t.Helper()
	cfg := acp.Config{
		Bin:               fakeAgent(t),
		ExpectedVersion:   "0.100.0",
		Cwd:               t.TempDir(),
		InitializeTimeout: 2 * time.Second,
		InputTimeout:      5 * time.Second,
	}
	if mode != "" {
		cfg.Env = []string{"FAKE_ACP_MODE=" + mode}
	}
	return &StepHandler{ProviderConfig: cfg, Spawner: acp.DefaultSpawner{}}
}

func TestStepHandlerRunsACPAndReportsDone(t *testing.T) {
	h := newTestStepHandler(t, "")
	sender := newRecordingSender()
	params := stepParams{
		ExecutionID: "exec-1", TaskID: "task-1", StepID: "step-1",
		AttemptNo: 1, Branch: "aladdin/x", Prompt: "hi",
	}
	out, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params))
	if err != nil || !out.Accepted {
		t.Fatalf("Handle: %+v, err=%v", out, err)
	}

	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "step_done" || last.executionID != "exec-1" || last.attempt != 1 || last.stepID != "step-1" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if last.data["branch"] != "aladdin/x" {
		t.Fatalf("branch = %v", last.data["branch"])
	}

	sender.mu.Lock()
	streams := append([]recordedStream(nil), sender.streams...)
	sender.mu.Unlock()
	if len(streams) == 0 {
		t.Fatal("没有流式内容")
	}
	if streams[0].delta != "hello" || streams[0].seq != 1 {
		t.Fatalf("首段流 = %+v", streams[0])
	}
	if !streams[len(streams)-1].final {
		t.Fatalf("末段流应为 final: %+v", streams[len(streams)-1])
	}
}

func TestStepHandlerReportsFailOnStartError(t *testing.T) {
	h := newTestStepHandler(t, "fail_init")
	sender := newRecordingSender()
	params := stepParams{ExecutionID: "exec-2", StepID: "step-2", AttemptNo: 1, Prompt: "hi"}
	if _, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params)); err != nil {
		t.Fatal(err)
	}

	sender.wait(t)
	last := sender.lastEvent()
	if last.kind != "step_fail" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if last.data["error_class"] != "runtime" {
		t.Fatalf("error_class = %v", last.data["error_class"])
	}
}

func TestStepHandlerRejectsUnknownMethod(t *testing.T) {
	h := newTestStepHandler(t, "")
	sender := newRecordingSender()
	payload, _ := json.Marshal(protocol.CommandPayload{Method: "stop_session"})

	out, err := h.Handle(WithSender(context.Background(), sender), protocol.Envelope{Type: protocol.TypeCommand, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted || out.Error == nil || out.Error.Code != "unsupported_method" {
		t.Fatalf("响应 = %+v", out)
	}
}

func TestStepHandlerRequiresSender(t *testing.T) {
	h := newTestStepHandler(t, "")
	params := stepParams{ExecutionID: "exec-3", StepID: "step-3", AttemptNo: 1, Prompt: "hi"}
	out, err := h.Handle(context.Background(), startEnvelope(t, params))
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted || out.Error == nil || out.Error.Code != "no_channel" {
		t.Fatalf("响应 = %+v", out)
	}
}

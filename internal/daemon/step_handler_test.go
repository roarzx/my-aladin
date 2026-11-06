package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	if last.data["commit_sha"] != "" || last.data["pr_url"] != "" {
		t.Fatalf("无仓库时不应有 commit/pr: %+v", last.data)
	}
	if digest, ok := last.data["summary_digest"].(string); !ok || len(digest) != 64 {
		t.Fatalf("summary_digest = %v", last.data["summary_digest"])
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
	payload, _ := json.Marshal(protocol.CommandPayload{Method: "bogus_method"})

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

// fakePRCreator 记录调用参数，测试中不访问 GitHub。
type fakePRCreator struct {
	mu    sync.Mutex
	url   string
	err   error
	calls int
	dir   string
	last  PRRequest
}

func (f *fakePRCreator) EnsurePR(_ context.Context, dir string, req PRRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.dir = dir
	f.last = req
	return f.url, f.err
}

func (f *fakePRCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// repoParams 构造一个带本地裸仓库的任务步骤。
func repoParams(origin, workRoot, taskID, branch string) stepParams {
	return stepParams{
		ExecutionID: "exec-repo", TaskID: taskID, StepID: "step-repo", AttemptNo: 1,
		Branch: branch, Prompt: "实现功能", WorkDir: workRoot,
		RepoURL: origin, RepoDefaultBranch: "main",
		SystemPrompt: "你是 Aladdin 编码 Agent。", TaskTitle: "实现登录", StepTitle: "写代码",
	}
}

func TestStepHandlerPreparesRepoAndReportsCommitAndPR(t *testing.T) {
	origin := setupOrigin(t)
	baseSHA := gitCmd(t, origin, "rev-parse", "refs/heads/main")
	workRoot := t.TempDir()
	taskID := "task-abcdefgh"
	branch := "aladdin/" + shortID(taskID)

	h := newTestStepHandler(t, "write_file")
	pr := &fakePRCreator{url: "https://github.com/acme/repo/pull/7"}
	h.PRCreator = pr

	sender := newRecordingSender()
	params := repoParams(origin, workRoot, taskID, branch)
	if _, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params)); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "step_done" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if last.data["branch"] != branch {
		t.Fatalf("branch = %v", last.data["branch"])
	}
	// fake agent 写入 agent-output.txt：应产生新提交并推送。
	commitSHA, _ := last.data["commit_sha"].(string)
	if commitSHA == "" || commitSHA == baseSHA {
		t.Fatalf("commit_sha = %v, 基线 %s", last.data["commit_sha"], baseSHA)
	}
	if digest, ok := last.data["summary_digest"].(string); !ok || len(digest) != 64 {
		t.Fatalf("summary_digest = %v", last.data["summary_digest"])
	}
	if last.data["pr_url"] != pr.url {
		t.Fatalf("pr_url = %v", last.data["pr_url"])
	}

	wantDir := filepath.Join(workRoot, "aladdin", shortID(taskID))
	pr.mu.Lock()
	gotDir, gotReq, calls := pr.dir, pr.last, pr.calls
	pr.mu.Unlock()
	if calls != 1 {
		t.Fatalf("PR 调用次数 = %d", calls)
	}
	if gotDir != wantDir {
		t.Fatalf("PR 工作目录 = %q, 期望 %q", gotDir, wantDir)
	}
	if gotReq.Base != "main" || gotReq.Head != branch || gotReq.RepoURL != origin || gotReq.Title != "实现登录" {
		t.Fatalf("PR 请求 = %+v", gotReq)
	}
	// 计划分支已推送到本地裸仓库。
	gitCmd(t, origin, "rev-parse", "--verify", "refs/heads/"+branch)
}

func TestStepHandlerFailsOnDirtyWorkspace(t *testing.T) {
	origin := setupOrigin(t)
	workRoot := t.TempDir()
	taskID := "task-dirtyxx"
	branch := "aladdin/" + shortID(taskID)
	dir := filepath.Join(workRoot, "aladdin", shortID(taskID))

	if _, err := Prepare(context.Background(), RepoSpec{URL: origin, Dir: dir, PlanBranch: branch}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user.txt"), []byte("user change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newTestStepHandler(t, "")
	pr := &fakePRCreator{url: "https://example.com/pr/1"}
	h.PRCreator = pr
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "step_fail" || last.data["error_class"] != "tool" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if pr.callCount() != 0 {
		t.Fatal("脏工作区不应创建 PR")
	}
}

func TestStepHandlerReportsToolFailWhenPRFails(t *testing.T) {
	origin := setupOrigin(t)
	workRoot := t.TempDir()
	taskID := "task-prfail"
	branch := "aladdin/" + shortID(taskID)

	h := newTestStepHandler(t, "write_file")
	h.PRCreator = &fakePRCreator{err: errors.New("gh pr create: boom")}
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "step_fail" || last.data["error_class"] != "tool" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if reason, _ := last.data["reason"].(string); !strings.Contains(reason, "boom") {
		t.Fatalf("reason = %v", last.data["reason"])
	}
}

func TestStepHandlerNoChangesSkipsPR(t *testing.T) {
	origin := setupOrigin(t)
	workRoot := t.TempDir()
	taskID := "task-nochange"
	branch := "aladdin/" + shortID(taskID)

	h := newTestStepHandler(t, "")
	pr := &fakePRCreator{url: "https://example.com/pr/1"}
	h.PRCreator = pr
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "step_done" || last.data["pr_url"] != "" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if pr.callCount() != 0 {
		t.Fatal("无改动步骤不应创建 PR")
	}
}

func TestPromptWithSystemPrefix(t *testing.T) {
	if got := promptWithSystem(stepParams{Prompt: "do"}); got != "do" {
		t.Fatalf("无 system_prompt 时应保持原样: %q", got)
	}
	got := promptWithSystem(stepParams{SystemPrompt: "sys", Prompt: "do"})
	if got != "sys\n\ndo" {
		t.Fatalf("prompt = %q", got)
	}
}

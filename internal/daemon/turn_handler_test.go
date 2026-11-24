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

func (s *recordingSender) SendEvent(kind, executionID string, data any) error {
	raw, _ := json.Marshal(data)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	s.mu.Lock()
	s.events = append(s.events, recordedEvent{kind: kind, executionID: executionID, data: fields})
	s.mu.Unlock()
	if kind == "turn_done" || kind == "turn_fail" {
		s.once.Do(func() { close(s.done) })
	}
	return nil
}

func (s *recordingSender) SendStream(executionID string, seq uint64, delta string, final bool) error {
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

func startEnvelope(t *testing.T, params turnParams) protocol.Envelope {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(protocol.CommandPayload{Method: "start_issue_turn", Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{Type: protocol.TypeCommand, Payload: payload}
}

func newTestTurnHandler(t *testing.T, mode string) *TurnHandler {
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
	return &TurnHandler{ProviderConfig: cfg, Spawner: acp.DefaultSpawner{}}
}

func TestTurnHandlerRunsACPAndReportsDone(t *testing.T) {
	h := newTestTurnHandler(t, "")
	sender := newRecordingSender()
	params := turnParams{
		ExecutionID: "exec-1", IssueID: "task-1",
		Branch: "aladdin/x", Prompt: "hi",
	}
	out, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params))
	if err != nil || !out.Accepted {
		t.Fatalf("Handle: %+v, err=%v", out, err)
	}

	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_done" || last.executionID != "exec-1" {
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

func TestTurnHandlerReportsFailOnStartError(t *testing.T) {
	h := newTestTurnHandler(t, "fail_init")
	sender := newRecordingSender()
	params := turnParams{ExecutionID: "exec-2", Prompt: "hi"}
	if _, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params)); err != nil {
		t.Fatal(err)
	}

	sender.wait(t)
	last := sender.lastEvent()
	if last.kind != "turn_fail" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if last.data["error_class"] != "runtime" {
		t.Fatalf("error_class = %v", last.data["error_class"])
	}
}

func TestTurnHandlerRejectsUnknownMethod(t *testing.T) {
	h := newTestTurnHandler(t, "")
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

func TestTurnHandlerRequiresSender(t *testing.T) {
	h := newTestTurnHandler(t, "")
	params := turnParams{ExecutionID: "exec-3", Prompt: "hi"}
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
func repoParams(origin, workRoot, taskID, branch string) turnParams {
	return turnParams{
		ExecutionID: "exec-repo", IssueID: taskID,
		Branch: branch, Prompt: "实现功能", WorkDir: workRoot,
		RepoURL: origin, RepoDefault: "main",
		SystemPrompt: "你是 Aladdin 编码 Agent。", IssueTitle: "实现登录",
	}
}

func TestTurnHandlerPreparesRepoAndReportsCommitAndPR(t *testing.T) {
	origin := setupOrigin(t)
	baseSHA := gitCmd(t, origin, "rev-parse", "refs/heads/main")
	workRoot := t.TempDir()
	taskID := "task-abcdefgh"
	branch := "aladdin/" + shortID(taskID)

	h := newTestTurnHandler(t, "write_file")
	pr := &fakePRCreator{url: "https://github.com/acme/repo/pull/7"}
	h.PRCreator = pr

	sender := newRecordingSender()
	params := repoParams(origin, workRoot, taskID, branch)
	if _, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params)); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_done" {
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

func TestTurnHandlerFailsOnDirtyWorkspace(t *testing.T) {
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

	h := newTestTurnHandler(t, "")
	pr := &fakePRCreator{url: "https://example.com/pr/1"}
	h.PRCreator = pr
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_fail" || last.data["error_class"] != "tool" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if pr.callCount() != 0 {
		t.Fatal("脏工作区不应创建 PR")
	}
}

func TestTurnHandlerReportsToolFailWhenPRFails(t *testing.T) {
	origin := setupOrigin(t)
	workRoot := t.TempDir()
	taskID := "task-prfail"
	branch := "aladdin/" + shortID(taskID)

	h := newTestTurnHandler(t, "write_file")
	h.PRCreator = &fakePRCreator{err: errors.New("gh pr create: boom")}
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_fail" || last.data["error_class"] != "tool" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if reason, _ := last.data["reason"].(string); !strings.Contains(reason, "boom") {
		t.Fatalf("reason = %v", last.data["reason"])
	}
}

func TestTurnHandlerNoChangesSkipsPR(t *testing.T) {
	origin := setupOrigin(t)
	workRoot := t.TempDir()
	taskID := "task-nochange"
	branch := "aladdin/" + shortID(taskID)

	h := newTestTurnHandler(t, "")
	pr := &fakePRCreator{url: "https://example.com/pr/1"}
	h.PRCreator = pr
	sender := newRecordingSender()
	if _, err := h.Handle(WithSender(context.Background(), sender),
		startEnvelope(t, repoParams(origin, workRoot, taskID, branch))); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_done" || last.data["pr_url"] != "" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if pr.callCount() != 0 {
		t.Fatal("无改动步骤不应创建 PR")
	}
}

func TestPromptWithSystemPrefix(t *testing.T) {
	h := &TurnHandler{}
	if got := h.promptWithSystem(turnParams{Prompt: "do"}); got != "do" {
		t.Fatalf("无 system_prompt 时应保持原样: %q", got)
	}
	got := h.promptWithSystem(turnParams{SystemPrompt: "sys", Prompt: "do"})
	if got != "sys\n\ndo" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestPlatformInstructionsAndEnv(t *testing.T) {
	h := &TurnHandler{APIURL: "http://ctl:8080", Token: "tok", CLIPath: "/opt/aladdin"}
	params := turnParams{IssueID: "issue-1", AgentID: "agent-1", ExecutionID: "exec-1", Prompt: "do"}

	env := h.platformEnv(params)
	want := map[string]string{
		"ALADDIN_API_URL":      "http://ctl:8080",
		"ALADDIN_TOKEN":        "tok",
		"ALADDIN_ISSUE_ID":     "issue-1",
		"ALADDIN_AGENT_ID":     "agent-1",
		"ALADDIN_EXECUTION_ID": "exec-1",
	}
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "PATH" {
			continue
		}
		if expected, known := want[key]; known && value != expected {
			t.Fatalf("%s = %q，期望 %q", key, value, expected)
		}
	}
	if !strings.HasPrefix(strings.Join(env, " "), "ALADDIN_API_URL=") && !containsEnv(env, "PATH=/opt/aladdin") {
		t.Fatalf("PATH 应包含 CLI 目录: %v", env)
	}

	prompt := h.promptWithSystem(params)
	if !strings.Contains(prompt, "issue update-status --id issue-1 --status completed") {
		t.Fatalf("提示词缺少上报说明: %s", prompt)
	}

	// 没有配置控制面地址时不注入，也不添加说明。
	bare := &TurnHandler{}
	if len(bare.platformEnv(params)) != 0 {
		t.Fatal("未配置 APIURL 时不应注入环境变量")
	}
	if got := bare.promptWithSystem(params); got != "do" {
		t.Fatalf("未配置 APIURL 时提示词应保持原样: %q", got)
	}
}

func containsEnv(env []string, prefix string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func TestSummarizeToolInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"bash", `{"command":"python3 -I hello.py","description":"跑一下"}`, "python3 -I hello.py"},
		{"bash argv", `{"command":["go","test","./..."]}`, "go test ./..."},
		{"write", `{"file_path":"/tmp/a.html","content":"<html>…"}`, "/tmp/a.html"},
		{"read", `{"path":"internal/x.go"}`, "internal/x.go"},
		{"web", `{"url":"https://example.com"}`, "https://example.com"},
		{"fallback", `{"foo":"bar"}`, `{"foo":"bar"}`},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeToolInput(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("summarize = %q，期望 %q", got, tc.want)
			}
		})
	}

	// 凭据必须脱敏后才进事件。
	got := summarizeToolInput(json.RawMessage(`{"command":"curl -H 'Authorization: Bearer sk-abcdef123456' https://x"}`))
	if strings.Contains(got, "sk-abcdef123456") {
		t.Fatalf("命令里的凭据未脱敏: %q", got)
	}
	// 超长输入截断。
	long := summarizeToolInput(json.RawMessage(`{"command":"` + strings.Repeat("a", 500) + `"}`))
	if len([]rune(long)) > 301 {
		t.Fatalf("超长命令未截断: %d", len([]rune(long)))
	}
}

func TestTurnOutputSplitsNarrationFromReply(t *testing.T) {
	summary := strings.Repeat("改完了，", 40) + "测试通过。"
	cases := []struct {
		name      string
		blocks    []string
		reply     string
		narration string
	}{
		{
			name:   "只有一段：整段就是回复",
			blocks: []string{"改好了，测试通过。"},
			reply:  "改好了，测试通过。",
		},
		{
			name:      "过程叙述与结论分开",
			blocks:    []string{"Let me look at the failing test first.", summary},
			reply:     summary,
			narration: "Let me look at the failing test first.",
		},
		{
			name:      "总结之后的 CLI 上报不算回复",
			blocks:    []string{"Let me read the file.", summary, "已上报 completed。"},
			reply:     summary + "\n\n已上报 completed。",
			narration: "Let me read the file.",
		},
		{
			name:      "全是短句：最后一段当回复",
			blocks:    []string{"先看看。", "好了。"},
			reply:     "好了。",
			narration: "先看看。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := &turnOutput{}
			for _, block := range tc.blocks {
				output.add(block)
				output.breakBlock() // 模拟每段文字后面都有一次工具调用
			}
			reply, narration := output.split()
			if reply != tc.reply {
				t.Fatalf("reply = %q，期望 %q", reply, tc.reply)
			}
			if narration != tc.narration {
				t.Fatalf("narration = %q，期望 %q", narration, tc.narration)
			}
		})
	}
}

func TestTurnOutputIgnoresBlankBlocks(t *testing.T) {
	output := &turnOutput{}
	output.add("   ")
	output.breakBlock()
	output.add("ok")
	reply, narration := output.split()
	if reply != "ok" || narration != "" {
		t.Fatalf("reply = %q, narration = %q", reply, narration)
	}
}

// 一轮里「叙述 → 工具 → 结论」时，turn_done 要把结论和过程分开回传。
func TestTurnHandlerSeparatesNarrationFromReply(t *testing.T) {
	h := newTestTurnHandler(t, "chatty")
	sender := newRecordingSender()
	params := turnParams{ExecutionID: "exec-9", IssueID: "task-9", Prompt: "hi"}

	if _, err := h.Handle(WithSender(context.Background(), sender), startEnvelope(t, params)); err != nil {
		t.Fatal(err)
	}
	sender.wait(t)

	last := sender.lastEvent()
	if last.kind != "turn_done" {
		t.Fatalf("终态事件 = %+v", last)
	}
	if got := last.data["reply"]; got != "修好了，测试通过。" {
		t.Fatalf("reply = %q，期望只包含结论", got)
	}
	if got := last.data["narration"]; got != "Let me look at the file first." {
		t.Fatalf("narration = %q", got)
	}
}

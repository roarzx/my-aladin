package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agentevent"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

var fakeAgentBin string

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	dir, err := os.MkdirTemp("", "fake-acp-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "临时目录: %v\n", err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "fakeagent")
	build := exec.Command("go", "build", "-o", bin, "./tests/fixtures/acp/fakeagent")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "构建 fake agent 失败: %v\n%s\n", err, out)
		os.Exit(1)
	}
	fakeAgentBin = bin

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func newTestProvider(t *testing.T, mode string, inputTimeout time.Duration) *Provider {
	t.Helper()
	return NewProvider(Config{
		Bin:               fakeAgentBin,
		ExpectedVersion:   "0.100.0",
		Cwd:               t.TempDir(),
		Env:               []string{"FAKE_ACP_MODE=" + mode},
		InitializeTimeout: 2 * time.Second,
		InputTimeout:      inputTimeout,
	}, DefaultSpawner{}, nil)
}

func TestStartAndPrompt(t *testing.T) {
	p := newTestProvider(t, "", 5*time.Second)
	ctx := context.Background()

	session, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = session.Close() }()
	if session.ACPID() != "acp-session-1" {
		t.Fatalf("ACPID = %q", session.ACPID())
	}

	var events []agentevent.Event
	result, err := session.Prompt(ctx, "hi", func(e agentevent.Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q", result.StopReason)
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
		t.Fatalf("Usage = %+v", result.Usage)
	}
	if len(events) != 2 {
		t.Fatalf("事件 = %+v", events)
	}
	if events[0].Kind != agentevent.KindStreamDelta || events[0].Text != "hello" {
		t.Fatalf("事件[0] = %+v", events[0])
	}
	if events[1].Kind != agentevent.KindToolCall || events[1].ToolCallID != "t1" {
		t.Fatalf("事件[1] = %+v", events[1])
	}
}

// TestClientReturns32601ForUnsupportedRequest 用内存管道确定性地验证：
// Agent 反向请求未知方法时，客户端返回 JSON-RPC -32601。
func TestClientReturns32601ForUnsupportedRequest(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	client := NewClient(clientSide)
	client.Start()
	defer func() {
		_ = client.Close()
		_ = serverSide.Close()
	}()

	request := `{"jsonrpc":"2.0","id":"req-1","method":"fs/read_text_file","params":{"path":"/tmp/x"}}` + "\n"
	if _, err := serverSide.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}

	_ = serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(serverSide).ReadString('\n')
	if err != nil {
		t.Fatalf("读取响应: %v", err)
	}
	var resp struct {
		ID    string `json:"id"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("解析响应: %v (%s)", err, line)
	}
	if resp.ID != "req-1" || resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("响应 = %s", line)
	}
}

func TestVersionMismatchRejected(t *testing.T) {
	p := newTestProvider(t, "", time.Second)
	p.cfg.ExpectedVersion = "9.9.9"

	_, err := p.Start(context.Background())
	assertCode(t, err, errcode.RuntimeStartFailed)
}

func TestInitializeFailure(t *testing.T) {
	p := newTestProvider(t, "fail_init", time.Second)

	_, err := p.Start(context.Background())
	assertCode(t, err, errcode.RuntimeStartFailed)
}

func TestValidate(t *testing.T) {
	if err := newTestProvider(t, "", time.Second).Validate(context.Background()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	err := newTestProvider(t, "fail_init", time.Second).Validate(context.Background())
	assertCode(t, err, errcode.RuntimeStartFailed)
}

func TestPromptTimeout(t *testing.T) {
	p := newTestProvider(t, "hang", 100*time.Millisecond)
	ctx := context.Background()

	session, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	_, err = session.Prompt(ctx, "hang", nil)
	if !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("期望 ErrPromptTimeout，实际: %v", err)
	}
}

func TestProcessCrashDetected(t *testing.T) {
	p := newTestProvider(t, "crash", 5*time.Second)
	ctx := context.Background()

	session, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Prompt(ctx, "crash", nil); err == nil {
		t.Fatal("子进程崩溃应返回错误")
	}
}

func TestResume(t *testing.T) {
	p := newTestProvider(t, "", time.Second)

	session, err := p.Resume(context.Background(), "acp-existing")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	defer func() { _ = session.Close() }()
	if session.ACPID() != "acp-existing" {
		t.Fatalf("ACPID = %q", session.ACPID())
	}
}

func TestRefusalStopReason(t *testing.T) {
	p := newTestProvider(t, "refuse", 5*time.Second)
	ctx := context.Background()

	session, err := p.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Prompt(ctx, "refuse", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "refusal" {
		t.Fatalf("StopReason = %q", result.StopReason)
	}
}

func assertCode(t *testing.T, err error, want errcode.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望失败 %s，实际成功", want)
	}
	e, ok := errcode.As(err)
	if !ok || e.Code != want {
		t.Fatalf("错误码 = %v，期望 %s", err, want)
	}
}

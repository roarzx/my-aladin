// Package acp 是 Claude Code 的 ACP 客户端与 Provider 适配器（C5/C6）。
//
// 报文按 9.7.1 归一化为 agentevent；适配器只做映射。
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/roarzx/my-aladin/internal/agentevent"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ProtocolVersion 是 v1 支持的 ACP 协议版本。
const ProtocolVersion = 1

var (
	// ErrPromptTimeout 表示 prompt 超过 input_timeout_seconds；调用方应按 uncertain 处理。
	ErrPromptTimeout = errors.New("acp: prompt 超时")
	// ErrResumeUnsupported 表示适配器不支持 session/load。
	ErrResumeUnsupported = errors.New("acp: 适配器不支持 session/load")
)

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// Spawner 负责启动与等待子进程；daemon 传入 Reaper 以获得进程组托管。
type Spawner interface {
	Start(cmd *exec.Cmd) error
	Wait(cmd *exec.Cmd) error
}

// DefaultSpawner 直接启动子进程（测试用）。
type DefaultSpawner struct{}

// Start 启动进程。
func (DefaultSpawner) Start(cmd *exec.Cmd) error { return cmd.Start() }

// Wait 等待进程。
func (DefaultSpawner) Wait(cmd *exec.Cmd) error { return cmd.Wait() }

// Config 是 Provider 启动配置。
type Config struct {
	Bin               string
	ExpectedVersion   string
	Cwd               string
	Env               []string
	MCPServers        []any
	InitializeTimeout time.Duration
	InputTimeout      time.Duration
}

// Provider 负责启动、探活与会话创建。
type Provider struct {
	cfg     Config
	spawner Spawner
	log     *slog.Logger
}

// NewProvider 构造适配器。
func NewProvider(cfg Config, spawner Spawner, logger *slog.Logger) *Provider {
	if spawner == nil {
		spawner = DefaultSpawner{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{cfg: cfg, spawner: spawner, log: logger}
}

// Validate 探活：启动并 initialize，成功后立即关闭（C6）。
func (p *Provider) Validate(ctx context.Context) error {
	session, err := p.start(ctx)
	if err != nil {
		return err
	}
	return session.Close()
}

// Start 启动并创建新会话。
func (p *Provider) Start(ctx context.Context) (*Session, error) {
	session, err := p.start(ctx)
	if err != nil {
		return nil, err
	}
	if err := session.New(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

// Resume 复用已有 ACP 会话；适配器不支持时返回 ErrResumeUnsupported。
func (p *Provider) Resume(ctx context.Context, acpSessionID string) (*Session, error) {
	session, err := p.start(ctx)
	if err != nil {
		return nil, err
	}
	if !session.loadSupported {
		_ = session.Close()
		return nil, ErrResumeUnsupported
	}
	if err := session.Load(ctx, acpSessionID); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

func (p *Provider) start(ctx context.Context) (*Session, error) {
	bin, err := p.resolveBin()
	if err != nil {
		return nil, err
	}
	if err := p.checkVersion(ctx, bin); err != nil {
		return nil, err
	}

	cmd := exec.Command(bin)
	cmd.Dir = p.cfg.Cwd
	if len(p.cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), p.cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errcode.Newf(errcode.RuntimeStartFailed, "创建 stdin 管道失败: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errcode.Newf(errcode.RuntimeStartFailed, "创建 stdout 管道失败: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := p.spawner.Start(cmd); err != nil {
		return nil, errcode.Newf(errcode.RuntimeStartFailed, "启动 Provider 失败: %v", err)
	}

	client := NewClient(&readWriteCloser{
		Reader:  stdout,
		Writer:  stdin,
		closers: []io.Closer{stdin},
	})
	session := &Session{provider: p, client: client, cmd: cmd}
	client.onNotification = session.handleNotification
	client.onRequest = session.handleRequest
	client.Start()

	initCtx := ctx
	if p.cfg.InitializeTimeout > 0 {
		var cancel context.CancelFunc
		initCtx, cancel = context.WithTimeout(ctx, p.cfg.InitializeTimeout)
		defer cancel()
	}
	raw, err := client.Call(initCtx, "initialize", map[string]any{
		"protocolVersion":    ProtocolVersion,
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		_ = session.Close()
		return nil, errcode.Newf(errcode.RuntimeStartFailed, "initialize 失败: %v", err)
	}

	var init struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		_ = session.Close()
		return nil, errcode.New(errcode.RuntimeStartFailed, "initialize 响应不合法")
	}
	if init.ProtocolVersion != ProtocolVersion {
		_ = session.Close()
		return nil, errcode.Newf(errcode.RuntimeStartFailed,
			"不支持的 ACP 协议版本 %d（期望 %d）", init.ProtocolVersion, ProtocolVersion)
	}
	session.loadSupported = init.AgentCapabilities.LoadSession
	return session, nil
}

func (p *Provider) resolveBin() (string, error) {
	name := p.cfg.Bin
	if name == "" {
		name = "claude-agent-acp"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errcode.Newf(errcode.RuntimeStartFailed, "找不到 Provider 可执行文件 %q", name)
	}
	return path, nil
}

func (p *Provider) checkVersion(ctx context.Context, bin string) error {
	if p.cfg.ExpectedVersion == "" {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, bin, "--version").CombinedOutput()
	if err != nil {
		p.log.Warn("无法获取 Provider 版本，跳过版本校验", "bin", bin, "err", err)
		return nil
	}
	actual := versionPattern.FindString(string(out))
	if actual == "" {
		p.log.Warn("Provider 版本输出无法解析，跳过校验", "output", strings.TrimSpace(string(out)))
		return nil
	}
	if actual != p.cfg.ExpectedVersion {
		return errcode.Newf(errcode.RuntimeStartFailed,
			"Provider 版本不匹配: 实际 %s, 期望 %s", actual, p.cfg.ExpectedVersion)
	}
	return nil
}

// Session 是一个已 initialize 的 ACP 会话。
type Session struct {
	provider      *Provider
	client        *Client
	cmd           *exec.Cmd
	loadSupported bool
	acpSessionID  string
	onEvent       func(agentevent.Event)
}

// ACPID 返回 Provider 侧会话 ID。
func (s *Session) ACPID() string { return s.acpSessionID }

// New 创建新会话。
func (s *Session) New(ctx context.Context) error {
	raw, err := s.client.Call(ctx, "session/new", map[string]any{
		"cwd":        s.provider.cfg.Cwd,
		"mcpServers": mcpServers(s.provider.cfg.MCPServers),
	})
	if err != nil {
		return errcode.Newf(errcode.RuntimeStartFailed, "session/new 失败: %v", err)
	}
	var res struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.SessionID == "" {
		return errcode.New(errcode.RuntimeStartFailed, "session/new 响应缺少 sessionId")
	}
	s.acpSessionID = res.SessionID
	return nil
}

// Load 复用已有会话。
func (s *Session) Load(ctx context.Context, acpSessionID string) error {
	_, err := s.client.Call(ctx, "session/load", map[string]any{
		"sessionId":  acpSessionID,
		"cwd":        s.provider.cfg.Cwd,
		"mcpServers": mcpServers(s.provider.cfg.MCPServers),
	})
	if err != nil {
		return errcode.Newf(errcode.UpstreamUnavailable, "session/load 失败: %v", err)
	}
	s.acpSessionID = acpSessionID
	return nil
}

// Usage 是 prompt 的用量（缺失时为 0）。
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Result 是 prompt 的终态。
type Result struct {
	StopReason string
	Usage      Usage
	Raw        json.RawMessage
}

// Prompt 执行一轮对话；事件通过 onEvent 同步回调。
func (s *Session) Prompt(ctx context.Context, text string, onEvent func(agentevent.Event)) (Result, error) {
	if s.acpSessionID == "" {
		return Result{}, errors.New("acp: 会话未创建")
	}
	s.onEvent = onEvent
	defer func() { s.onEvent = nil }()

	callCtx := ctx
	if timeout := s.provider.cfg.InputTimeout; timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	raw, err := s.client.Call(callCtx, "session/prompt", map[string]any{
		"sessionId": s.acpSessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	})
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			_ = s.Cancel(context.Background())
			return Result{}, ErrPromptTimeout
		}
		return Result{}, err
	}

	var res struct {
		StopReason string `json:"stopReason"`
		Usage      struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(raw, &res)
	return Result{
		StopReason: res.StopReason,
		Usage:      Usage{InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens},
		Raw:        raw,
	}, nil
}

// Cancel 发送取消通知。
func (s *Session) Cancel(ctx context.Context) error {
	if s.acpSessionID == "" {
		return nil
	}
	return s.client.Notify("session/cancel", map[string]any{"sessionId": s.acpSessionID})
}

// Close 关闭连接并回收子进程。
func (s *Session) Close() error {
	_ = s.client.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.cmd != nil {
		_ = s.provider.spawner.Wait(s.cmd)
	}
	return nil
}

func (s *Session) handleNotification(method string, params json.RawMessage) {
	if method != "session/update" || s.onEvent == nil {
		return
	}
	if event, ok := NormalizeUpdate(params); ok {
		s.onEvent(event)
	}
}

// handleRequest 对 Agent 反向请求做 fail-closed 处理（9.7.1 v1 不启用 fs/terminal）。
func (s *Session) handleRequest(method string, _ json.RawMessage) (json.RawMessage, *rpcError) {
	switch method {
	case "session/request_permission":
		return json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "unsupported method"}
	}
}

func mcpServers(servers []any) []any {
	if servers == nil {
		return []any{}
	}
	return servers
}

type readWriteCloser struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (rw *readWriteCloser) Close() error {
	var errs []error
	for _, closer := range rw.closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

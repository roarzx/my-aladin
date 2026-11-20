package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/roarzx/my-aladin/internal/agentevent"
	"github.com/roarzx/my-aladin/internal/transport/acp"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// TurnHandler 处理服务端的 start_issue_turn：复用（或新建）ACP 会话，
// 把一条消息交给 Agent，执行完回传 turn_done / turn_fail。
type TurnHandler struct {
	ProviderConfig acp.Config
	Spawner        acp.Spawner
	// PRCreator 覆盖 gh 实现；为空时使用 GHCreator（测试注入用）。
	PRCreator PRCreator
	// Running 登记本机执行，供心跳续约与 stop_session 使用。
	Running *RunningExecutions
	// Artifacts 上报产物；为空时跳过（例如未配置 APIURL）。
	Artifacts ArtifactUploader
	// APIURL / Token 用于把平台 CLI 需要的上下文注入到 Agent 进程。
	APIURL string
	Token  string
	// CLIPath 覆盖包含 aladdin 可执行文件的目录；为空时取 daemon 所在目录。
	CLIPath string
	Log     *slog.Logger
}

type turnParams struct {
	ExecutionID string `json:"execution_id"`
	IssueID     string `json:"issue_id"`
	AgentID     string `json:"agent_id"`
	// SessionID 是服务端会话行 ID；ACPSessionID 非空表示可复用上一轮会话。
	SessionID    string `json:"session_id"`
	ACPSessionID string `json:"acp_session_id"`
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"system_prompt"`
	WorkDir      string `json:"work_dir"`
	RepoURL      string `json:"repo_url"`
	RepoDefault  string `json:"repo_default_branch"`
	Branch       string `json:"branch"`
	IssueTitle   string `json:"issue_title"`
}

// Handle 实现 CommandHandler；立即接受，执行在后台进行。
func (h *TurnHandler) Handle(ctx context.Context, env protocol.Envelope) (protocol.ResponsePayload, error) {
	var command protocol.CommandPayload
	if err := json.Unmarshal(env.Payload, &command); err != nil {
		return rejected("invalid_command", "payload 不合法"), nil
	}
	switch command.Method {
	case "start_issue_turn":
		return h.start(ctx, command.Params), nil
	case "stop_session":
		var params struct {
			ExecutionID string `json:"execution_id"`
		}
		if err := json.Unmarshal(command.Params, &params); err != nil || params.ExecutionID == "" {
			return rejected("invalid_params", "params 不合法"), nil
		}
		if h.Running == nil || !h.Running.Cancel(params.ExecutionID) {
			return rejected("not_running", "执行不在本机运行"), nil
		}
		return protocol.ResponsePayload{Accepted: true}, nil
	default:
		return rejected("unsupported_method", "未注册命令: "+command.Method), nil
	}
}

func (h *TurnHandler) start(ctx context.Context, raw json.RawMessage) protocol.ResponsePayload {
	var params turnParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return rejected("invalid_params", "params 不合法")
	}
	sender, ok := SenderFrom(ctx)
	if !ok {
		return rejected("no_channel", "连接不支持事件回传")
	}
	if strings.TrimSpace(params.Prompt) == "" {
		return rejected("invalid_params", "提示词为空，拒绝执行")
	}

	go h.run(ctx, params, sender)
	return protocol.ResponsePayload{Accepted: true}
}

func (h *TurnHandler) run(ctx context.Context, params turnParams, sender Sender) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.Running != nil {
		unregister := h.Running.Start(params.ExecutionID, 1, cancel)
		defer unregister()
	}

	workDir, err := h.workspaceDir(params)
	if err != nil {
		h.fail(sender, params, string(errcode.ClassTool), false, err)
		return
	}

	// Provider 配置在多个执行间共享，必须复制后再改 Cwd/Env。
	cfg := h.ProviderConfig
	cfg.Cwd = workDir
	cfg.Env = append(cfg.Env, h.platformEnv(params)...)

	var workspace *Workspace
	if params.RepoURL != "" {
		workspace, err = Prepare(ctx, RepoSpec{
			URL:        params.RepoURL,
			Dir:        workDir,
			PlanBranch: params.Branch,
		})
		if err != nil {
			h.fail(sender, params, string(errcode.ClassTool), false, err)
			return
		}
		// 不清理用户改动：脏工作区直接失败。
		if err := workspace.EnsureClean(ctx); err != nil {
			h.fail(sender, params, string(errcode.ClassTool), false, err)
			return
		}
	}

	provider := acp.NewProvider(cfg, h.Spawner, h.log())

	// 长会话：优先复用上一轮的 ACP 会话，适配器不支持时退回新会话。
	session, err := h.openSession(ctx, provider, params.ACPSessionID)
	if err != nil {
		h.fail(sender, params, string(errcode.Classify(err)), false, err)
		return
	}
	defer func() { _ = session.Close() }()

	var seq atomic.Uint64
	digest := sha256.New()
	runStarted := time.Now()
	// 工具调用元信息分散在同一 tool_call_id 的多次 update 里：首帧带 title，
	// 终帧只带 status，必须累积后再上报。
	tools := make(map[string]*toolState)
	emitWork := func(kind string, data map[string]any) {
		if err := sender.SendEvent("work_event", params.ExecutionID,
			map[string]any{"kind": kind, "data": data}); err != nil {
			h.log().Warn("发送过程事件失败", "kind", kind, "err", err)
		}
	}

	// 汇总本轮助手输出，随 turn_done 回传，用于会话页展示「回复」。
	var reply strings.Builder
	result, err := session.Prompt(ctx, h.promptWithSystem(params), func(event agentevent.Event) {
		switch event.Kind {
		case agentevent.KindToolCall, agentevent.KindToolUpdate:
			handleTool(tools, emitWork, event)
			return
		case agentevent.KindNotice:
			emitWork("agent.notice", map[string]any{"text": event.Text, "status": "info"})
			return
		case agentevent.KindStreamDelta, agentevent.KindThoughtDelta:
			// 仅这两种进入流式输出
		default:
			return
		}
		if event.Kind == agentevent.KindStreamDelta {
			_, _ = digest.Write([]byte(event.Text))
			if reply.Len() < maxReplyBytes {
				reply.WriteString(event.Text)
			}
		}
		if err := sender.SendStream(params.ExecutionID, seq.Add(1), event.Text, false); err != nil {
			h.log().Warn("发送流式内容失败", "err", err)
		}
	})
	if err != nil {
		h.fail(sender, params, string(errcode.Classify(err)), isUncertain(err), err)
		return
	}

	if err := sender.SendStream(params.ExecutionID, seq.Add(1), "", true); err != nil {
		h.log().Warn("发送流式结束失败", "err", err)
	}

	summaryDigest := hex.EncodeToString(digest.Sum(nil))

	switch result.StopReason {
	case "end_turn":
		commitSHA, prURL := "", ""
		if workspace != nil {
			commitSHA, prURL, err = h.deliver(ctx, workspace, params)
			if err != nil {
				h.fail(sender, params, string(errcode.ClassTool), false, err)
				return
			}
		}
		// 产物上传失败不影响结果，只记日志。
		h.uploadArtifacts(ctx, params, workDir, runStarted)
		if err := sender.SendEvent("turn_done", params.ExecutionID, map[string]any{
			"summary_digest": summaryDigest,
			"commit_sha":     commitSHA,
			"branch":         params.Branch,
			"pr_url":         prURL,
			"input_tokens":   result.Usage.InputTokens,
			"output_tokens":  result.Usage.OutputTokens,
			"duration_ms":    time.Since(runStarted).Milliseconds(),
			"reply":          reply.String(),
			"acp_session_id": session.ACPID(),
		}); err != nil {
			h.log().Warn("发送 turn_done 失败", "err", err)
		}
	default:
		h.fail(sender, params, "verification", false,
			fmt.Errorf("prompt 以 %q 结束", result.StopReason))
	}
}

// openSession 复用已有 ACP 会话；没有或不支持时新建。
func (h *TurnHandler) openSession(ctx context.Context, provider *acp.Provider,
	acpSessionID string) (*acp.Session, error) {
	if acpSessionID == "" {
		return provider.Start(ctx)
	}
	session, err := provider.Resume(ctx, acpSessionID)
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, acp.ErrResumeUnsupported) {
		h.log().Warn("恢复会话失败，改用新会话", "session", acpSessionID, "err", err)
	}
	return provider.Start(ctx)
}

// toolState 累积一次工具调用的元信息。
type toolState struct {
	startedAt time.Time
	title     string
	kind      string
}

// maxReplyBytes 限制随 turn_done 回传的助手文本长度（会话展示用）。
const maxReplyBytes = 8000

func handleTool(tools map[string]*toolState, emitWork func(string, map[string]any), event agentevent.Event) {
	if event.ToolCallID == "" {
		return
	}
	state := tools[event.ToolCallID]
	if state == nil {
		state = &toolState{startedAt: time.Now()}
		tools[event.ToolCallID] = state
	}
	if event.Title != "" {
		state.title = event.Title
	}
	if event.ToolKind != "" {
		state.kind = event.ToolKind
	}
	status := strings.ToLower(strings.TrimSpace(event.Status))
	if status != "completed" && status != "failed" {
		return
	}
	delete(tools, event.ToolCallID)
	emitWork("tool.call", map[string]any{
		"tool_call_id": event.ToolCallID,
		"title":        state.title,
		"tool_kind":    state.kind,
		"status":       status,
		"duration_ms":  time.Since(state.startedAt).Milliseconds(),
	})
}

// uploadArtifacts 把本次执行期间新增/修改的文件上报给控制面。
func (h *TurnHandler) uploadArtifacts(ctx context.Context, params turnParams, workDir string, since time.Time) {
	if h.Artifacts == nil || workDir == "" {
		return
	}
	files := collectArtifacts(workDir, since)
	for _, file := range files {
		if err := h.Artifacts.UploadArtifact(ctx, params.ExecutionID,
			file.relPath, file.contentType, file.data); err != nil {
			h.log().Warn("上传产物失败", "path", file.relPath, "err", err)
		}
	}
	if len(files) > 0 {
		h.log().Info("产物已上报", "issue", params.IssueID, "count", len(files))
	}
}

// deliver 提交、推送分支并确保 PR 存在；返回 commit_sha 与 pr_url。
func (h *TurnHandler) deliver(ctx context.Context, ws *Workspace, params turnParams) (string, string, error) {
	changed, err := ws.HasChanges(ctx)
	if err != nil {
		return "", "", err
	}
	commitSHA, err := ws.Head(ctx)
	if err != nil {
		return "", "", err
	}
	if !changed {
		// 本轮没有产生改动：不 push、不建 PR。
		return commitSHA, "", nil
	}
	commitSHA, err = ws.CommitAll(ctx, commitMessage(params))
	if err != nil {
		return "", "", err
	}

	if params.Branch == "" {
		h.log().Warn("缺少工作分支，跳过推送与 PR", "issue", params.IssueID)
		return commitSHA, "", nil
	}
	if err := ws.Push(ctx, params.Branch); err != nil {
		return "", "", err
	}

	creator := h.PRCreator
	if creator == nil {
		if os.Getenv("ALADDIN_PR_PROVIDER") == "local" {
			creator = LocalPRCreator{}
		} else {
			creator = GHCreator{}
		}
	}
	prURL, err := creator.EnsurePR(ctx, ws.Dir, PRRequest{
		RepoURL: params.RepoURL,
		Base:    params.RepoDefault,
		Head:    params.Branch,
		Title:   params.IssueTitle,
		Body:    prBody(params),
	})
	if err != nil {
		return "", "", err
	}
	return commitSHA, prURL, nil
}

// workspaceDir 计算本次执行的工作目录。
//
// 有仓库时为 <work_dir>/aladdin/<task 前 8 位>，无仓库时直接用 work_dir；
// 两者都为空时回落到 Provider 配置的 Cwd（兼容旧调用）。
func (h *TurnHandler) workspaceDir(params turnParams) (string, error) {
	if params.RepoURL == "" {
		if params.WorkDir == "" {
			return h.ProviderConfig.Cwd, nil
		}
		if !filepath.IsAbs(params.WorkDir) {
			return "", fmt.Errorf("work_dir 必须是绝对路径: %q", params.WorkDir)
		}
		return params.WorkDir, nil
	}
	if params.WorkDir == "" {
		return "", errors.New("有仓库时必须提供 work_dir")
	}
	if !filepath.IsAbs(params.WorkDir) {
		return "", fmt.Errorf("work_dir 必须是绝对路径: %q", params.WorkDir)
	}
	return filepath.Join(params.WorkDir, "aladdin", shortID(params.IssueID)), nil
}

// promptWithSystem 拼装最终提示词：系统职责 + 本轮内容 + 平台上报说明。
func (h *TurnHandler) promptWithSystem(params turnParams) string {
	parts := make([]string, 0, 3)
	if params.SystemPrompt != "" {
		parts = append(parts, params.SystemPrompt)
	}
	parts = append(parts, params.Prompt)
	if block := h.platformInstructions(params); block != "" {
		parts = append(parts, block)
	}
	return strings.Join(parts, "\n\n")
}

// platformEnv 把 Issue 上下文与机器令牌注入 Agent 进程。
//
// 未配置 APIURL/Token 时不注入：Agent 没有上报通道，也不该看到一个
// 调用必然失败的 CLI。
func (h *TurnHandler) platformEnv(params turnParams) []string {
	if h.APIURL == "" || h.Token == "" || params.IssueID == "" {
		return nil
	}
	env := []string{
		"ALADDIN_API_URL=" + h.APIURL,
		"ALADDIN_TOKEN=" + h.Token,
		"ALADDIN_ISSUE_ID=" + params.IssueID,
		"ALADDIN_TASK_ID=" + params.IssueID,
		"ALADDIN_AGENT_ID=" + params.AgentID,
		"ALADDIN_EXECUTION_ID=" + params.ExecutionID,
		"ALADDIN_SESSION_ID=" + params.SessionID,
	}
	if dir := h.cliDir(); dir != "" {
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}

// cliDir 返回包含 aladdin 可执行文件的目录；找不到时返回空串。
func (h *TurnHandler) cliDir() string {
	if h.CLIPath != "" {
		return h.CLIPath
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "aladdin")); err != nil {
		return ""
	}
	return dir
}

// platformInstructions 告诉 Agent 唯一的完成通道：CLI 上报。
func (h *TurnHandler) platformInstructions(params turnParams) string {
	if h.APIURL == "" || h.Token == "" || params.IssueID == "" {
		return ""
	}
	issueID := params.IssueID
	return strings.Join([]string{
		"## 平台上报（必须执行）",
		"",
		"本次执行的 Issue：" + issueID,
		"平台只能通过 CLI 得知你的结果；不调用下面的命令，Issue 会一直停留在当前状态。",
		"`aladdin` 已在 PATH 中，令牌由环境变量 ALADDIN_TOKEN 注入，不要自行传 --token。",
		"",
		"完成并自检通过后：",
		"  aladdin openapi issue update-status --id " + issueID +
			" --status completed --reason \"做了什么、怎么验证的\"",
		"卡住、需要用户决策：",
		"  aladdin openapi issue update-status --id " + issueID +
			" --status blocked --reason \"卡在哪、需要什么\"",
		"无法继续且不是阻塞问题：",
		"  aladdin openapi issue update-status --id " + issueID +
			" --status failed --reason \"失败原因\"",
		"需要用户补充信息时，也可以先发结构化提问：",
		"  aladdin openapi issue input request --message \"你的问题\"",
		"",
		"只有真正完成并验证过才上报 completed；没做完就如实上报 blocked/failed。",
	}, "\n")
}

func commitMessage(params turnParams) string {
	title := strings.TrimSpace(params.IssueTitle)
	if title == "" {
		title = "自动提交"
	}
	return "aladdin: " + title
}

func prBody(params turnParams) string {
	body := "Aladdin 自动提交"
	if params.IssueTitle != "" {
		body += "\n\nIssue：" + params.IssueTitle
	}
	if params.IssueID != "" {
		body += "\n执行：" + params.ExecutionID
	}
	return body
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (h *TurnHandler) fail(sender Sender, params turnParams, class string, uncertain bool, err error) {
	h.log().Warn("执行失败", "issue", params.IssueID, "err", err)
	if sendErr := sender.SendEvent("turn_fail", params.ExecutionID, map[string]any{
		"reason":      err.Error(),
		"error_class": class,
		"uncertain":   uncertain,
	}); sendErr != nil {
		h.log().Warn("发送 turn_fail 失败", "err", sendErr)
	}
}

func (h *TurnHandler) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

func isUncertain(err error) bool {
	return errors.Is(err, acp.ErrPromptTimeout)
}

func rejected(code, message string) protocol.ResponsePayload {
	return protocol.ResponsePayload{
		Accepted: false,
		Error:    &protocol.ResponseError{Code: code, Message: message},
	}
}

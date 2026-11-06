package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/roarzx/my-aladin/internal/agentevent"
	"github.com/roarzx/my-aladin/internal/transport/acp"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// StepHandler 处理服务端的 start_step_session：拉起 ACP Provider、执行 prompt 并回传结果。
type StepHandler struct {
	ProviderConfig acp.Config
	Spawner        acp.Spawner
	// PRCreator 覆盖 gh 实现；为空时使用 GHCreator（测试注入用）。
	PRCreator PRCreator
	// Running 登记本机执行，供心跳续约与 stop_session 使用（E4/E9）。
	Running *RunningExecutions
	Log     *slog.Logger
}

type stepParams struct {
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id"`
	StepID      string `json:"step_id"`
	AttemptNo   uint32 `json:"attempt_no"`
	LeaseEpoch  uint64 `json:"lease_epoch"`
	Branch      string `json:"branch"`
	Prompt      string `json:"prompt"`
	// 下列字段由控制面在派发时补齐（主路径计划第四节）。
	WorkDir           string `json:"work_dir"`
	RepoURL           string `json:"repo_url"`
	RepoDefaultBranch string `json:"repo_default_branch"`
	SystemPrompt      string `json:"system_prompt"`
	TaskTitle         string `json:"task_title"`
	StepTitle         string `json:"step_title"`
}

// Handle 实现 CommandHandler；立即接受，执行在后台进行。
func (h *StepHandler) Handle(ctx context.Context, env protocol.Envelope) (protocol.ResponsePayload, error) {
	var command protocol.CommandPayload
	if err := json.Unmarshal(env.Payload, &command); err != nil {
		return rejected("invalid_command", "payload 不合法"), nil
	}
	switch command.Method {
	case "start_step_session":
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

func (h *StepHandler) start(ctx context.Context, raw json.RawMessage) protocol.ResponsePayload {
	var params stepParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return rejected("invalid_params", "params 不合法")
	}
	sender, ok := SenderFrom(ctx)
	if !ok {
		return rejected("no_channel", "连接不支持事件回传")
	}

	go h.run(ctx, params, sender)
	return protocol.ResponsePayload{Accepted: true}
}

func (h *StepHandler) run(ctx context.Context, params stepParams, sender Sender) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.Running != nil {
		epoch := params.LeaseEpoch
		if epoch == 0 {
			epoch = 1
		}
		unregister := h.Running.Start(params.ExecutionID, epoch, cancel)
		defer unregister()
	}

	workDir, err := h.workspaceDir(params)
	if err != nil {
		h.fail(sender, params, string(errcode.ClassTool), false, err)
		return
	}

	// Provider 配置在多个执行间共享，必须复制后再改 Cwd。
	cfg := h.ProviderConfig
	cfg.Cwd = workDir

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

	session, err := provider.Start(ctx)
	if err != nil {
		h.fail(sender, params, string(errcode.Classify(err)), false, err)
		return
	}
	defer func() { _ = session.Close() }()

	var seq atomic.Uint64
	digest := sha256.New()
	result, err := session.Prompt(ctx, promptWithSystem(params), func(event agentevent.Event) {
		if event.Kind != agentevent.KindStreamDelta && event.Kind != agentevent.KindThoughtDelta {
			return
		}
		if event.Kind == agentevent.KindStreamDelta {
			_, _ = digest.Write([]byte(event.Text))
		}
		_ = sender.SendStream(params.ExecutionID, params.AttemptNo, params.StepID,
			seq.Add(1), event.Text, false)
	})
	if err != nil {
		h.fail(sender, params, string(errcode.Classify(err)), isUncertain(err), err)
		return
	}

	_ = sender.SendStream(params.ExecutionID, params.AttemptNo, params.StepID,
		seq.Add(1), "", true)

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
		_ = sender.SendEvent("step_done", params.ExecutionID, params.AttemptNo, params.StepID, map[string]any{
			"attempt_no":     params.AttemptNo,
			"summary_digest": summaryDigest,
			"commit_sha":     commitSHA,
			"branch":         params.Branch,
			"pr_url":         prURL,
		})
	default:
		h.fail(sender, params, "verification", false,
			fmt.Errorf("prompt 以 %q 结束", result.StopReason))
	}
}

// deliver 提交、推送计划分支并确保 PR 存在；返回 commit_sha 与 pr_url。
func (h *StepHandler) deliver(ctx context.Context, ws *Workspace, params stepParams) (string, string, error) {
	changed, err := ws.HasChanges(ctx)
	if err != nil {
		return "", "", err
	}
	commitSHA, err := ws.Head(ctx)
	if err != nil {
		return "", "", err
	}
	if !changed {
		// 本步没有产生改动：不 push、不建 PR（多步计划中后续步骤常见）
		return commitSHA, "", nil
	}
	commitSHA, err = ws.CommitAll(ctx, commitMessage(params))
	if err != nil {
		return "", "", err
	}

	if params.Branch == "" {
		h.log().Warn("缺少计划分支，跳过推送与 PR", "step", params.StepID)
		return commitSHA, "", nil
	}
	if err := ws.Push(ctx, params.Branch); err != nil {
		return "", "", err
	}

	creator := h.PRCreator
	if creator == nil {
		creator = GHCreator{}
	}
	prURL, err := creator.EnsurePR(ctx, ws.Dir, PRRequest{
		RepoURL: params.RepoURL,
		Base:    params.RepoDefaultBranch,
		Head:    params.Branch,
		Title:   params.TaskTitle,
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
func (h *StepHandler) workspaceDir(params stepParams) (string, error) {
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
	return filepath.Join(params.WorkDir, "aladdin", shortID(params.TaskID)), nil
}

func promptWithSystem(params stepParams) string {
	if params.SystemPrompt == "" {
		return params.Prompt
	}
	return params.SystemPrompt + "\n\n" + params.Prompt
}

func commitMessage(params stepParams) string {
	title := strings.TrimSpace(params.TaskTitle)
	if title == "" {
		title = strings.TrimSpace(params.StepTitle)
	}
	if title == "" {
		title = "自动提交"
	}
	return "aladdin: " + title
}

func prBody(params stepParams) string {
	body := "Aladdin 自动提交"
	if params.StepTitle != "" {
		body += "\n\n步骤：" + params.StepTitle
	}
	if params.ExecutionID != "" {
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

func (h *StepHandler) fail(sender Sender, params stepParams, class string, uncertain bool, err error) {
	h.log().Warn("步骤执行失败", "step", params.StepID, "err", err)
	_ = sender.SendEvent("step_fail", params.ExecutionID, params.AttemptNo, params.StepID, map[string]any{
		"attempt_no":  params.AttemptNo,
		"reason":      err.Error(),
		"error_class": class,
		"uncertain":   uncertain,
	})
}

func (h *StepHandler) log() *slog.Logger {
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

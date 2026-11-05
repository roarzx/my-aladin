package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

	provider := acp.NewProvider(h.ProviderConfig, h.Spawner, h.log())

	session, err := provider.Start(ctx)
	if err != nil {
		h.fail(sender, params, string(errcode.Classify(err)), false, err)
		return
	}
	defer func() { _ = session.Close() }()

	var seq atomic.Uint64
	result, err := session.Prompt(ctx, params.Prompt, func(event agentevent.Event) {
		if event.Kind != agentevent.KindStreamDelta && event.Kind != agentevent.KindThoughtDelta {
			return
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

	switch result.StopReason {
	case "end_turn":
		_ = sender.SendEvent("step_done", params.ExecutionID, params.AttemptNo, params.StepID, map[string]any{
			"attempt_no":     params.AttemptNo,
			"summary_digest": "",
			"commit_sha":     "",
			"branch":         params.Branch,
		})
	default:
		h.fail(sender, params, "verification", false,
			fmt.Errorf("prompt 以 %q 结束", result.StopReason))
	}
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

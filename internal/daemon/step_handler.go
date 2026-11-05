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
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// StepHandler 处理服务端的 start_step_session：拉起 ACP Provider、执行 prompt 并回传结果。
type StepHandler struct {
	ProviderConfig acp.Config
	Spawner        acp.Spawner
	Log            *slog.Logger
}

type stepParams struct {
	ExecutionID string `json:"execution_id"`
	TaskID      string `json:"task_id"`
	StepID      string `json:"step_id"`
	AttemptNo   uint32 `json:"attempt_no"`
	Branch      string `json:"branch"`
	Prompt      string `json:"prompt"`
}

// Handle 实现 CommandHandler；立即接受，执行在后台进行。
func (h *StepHandler) Handle(ctx context.Context, env protocol.Envelope) (protocol.ResponsePayload, error) {
	var command protocol.CommandPayload
	if err := json.Unmarshal(env.Payload, &command); err != nil {
		return rejected("invalid_command", "payload 不合法"), nil
	}
	if command.Method != "start_step_session" {
		return rejected("unsupported_method", "未注册命令: "+command.Method), nil
	}
	var params stepParams
	if err := json.Unmarshal(command.Params, &params); err != nil {
		return rejected("invalid_params", "params 不合法"), nil
	}
	sender, ok := SenderFrom(ctx)
	if !ok {
		return rejected("no_channel", "连接不支持事件回传"), nil
	}

	go h.run(ctx, params, sender)
	return protocol.ResponsePayload{Accepted: true}, nil
}

func (h *StepHandler) run(ctx context.Context, params stepParams, sender Sender) {
	provider := acp.NewProvider(h.ProviderConfig, h.Spawner, h.log())

	session, err := provider.Start(ctx)
	if err != nil {
		h.fail(sender, params, "runtime", false, err)
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
		h.fail(sender, params, "runtime", isUncertain(err), err)
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

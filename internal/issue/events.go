package issue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// EventHandler 处理 daemon 上行的 turn_done / turn_fail / 过程事件。
type EventHandler struct {
	Service *Service
	Log     *slog.Logger
}

// Handle 实现 ws.Hub 的事件回调。
func (h *EventHandler) Handle(ctx context.Context, _ string, env protocol.Envelope) error {
	var payload protocol.EventPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}
	h.logger().Debug("处理上行事件", "kind", payload.Kind, "execution", env.ExecutionID)

	data := map[string]any{}
	if len(payload.Data) > 0 {
		_ = json.Unmarshal(payload.Data, &data)
	}
	switch payload.Kind {
	case "":
		// 流式帧没有 kind：用它把执行从 dispatched 推进到 running，
		// 否则整轮跑完前页面一直显示「已派发」。
		if env.Type == protocol.TypeStream {
			h.Service.MarkRunning(ctx, env.ExecutionID)
		}
	case "turn_done":
		return h.Service.ApplyTurnDone(ctx, env.ExecutionID, data)
	case "turn_fail":
		return h.Service.ApplyTurnFail(ctx, env.ExecutionID, data)
	case "work_event":
		return h.workEvent(ctx, env.ExecutionID, data)
	case "stream":
		h.Service.MarkRunning(ctx, env.ExecutionID)
	}
	return nil
}

// workEvent 把执行端的过程事件（工具调用、通知）落为活动记录，供审计时间线展示。
func (h *EventHandler) workEvent(ctx context.Context, executionID string, data map[string]any) error {
	kind, _ := data["kind"].(string)
	if kind == "" {
		return nil
	}
	payload, _ := data["data"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	run, err := mysqlstore.GetIssueRunByExecution(ctx, h.Service.DB, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// 过程事件不再带步骤语义，只保留执行归属。
	delete(payload, "step_id")
	delete(payload, "attempt_no")
	payload["run_id"] = run.ID
	_, _ = mysqlstore.AppendActivity(ctx, h.Service.DB, run.TaskID, kind, payload)
	return nil
}

func (h *EventHandler) logger() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

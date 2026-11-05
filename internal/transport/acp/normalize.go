package acp

import (
	"encoding/json"

	"github.com/roarzx/my-aladin/internal/agentevent"
)

// updateParams 是 session/update 通知的参数结构（只取 v1 需要的字段）。
type updateParams struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		SessionUpdate string `json:"sessionUpdate"`
		Content       struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		ToolCallID string `json:"toolCallId"`
		Title      string `json:"title"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
	} `json:"update"`
}

// NormalizeUpdate 把 ACP 的 session/update 映射为归一化事件；未知类型返回 false。
func NormalizeUpdate(params json.RawMessage) (agentevent.Event, bool) {
	var p updateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return agentevent.Event{}, false
	}

	event := agentevent.Event{
		SessionID: p.SessionID,
		Raw:       params,
	}
	switch p.Update.SessionUpdate {
	case "agent_message_chunk":
		event.Kind = agentevent.KindStreamDelta
		event.Text = p.Update.Content.Text
	case "agent_thought_chunk":
		event.Kind = agentevent.KindThoughtDelta
		event.Text = p.Update.Content.Text
	case "tool_call":
		event.Kind = agentevent.KindToolCall
		event.ToolCallID = p.Update.ToolCallID
		event.Title = p.Update.Title
		event.ToolKind = p.Update.Kind
		event.Status = p.Update.Status
	case "tool_call_update":
		event.Kind = agentevent.KindToolUpdate
		event.ToolCallID = p.Update.ToolCallID
		event.Title = p.Update.Title
		event.ToolKind = p.Update.Kind
		event.Status = p.Update.Status
	case "plan":
		event.Kind = agentevent.KindPlan
	default:
		return agentevent.Event{}, false
	}
	return event, true
}

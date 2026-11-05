// Package agentevent 是 daemon 内部消费的归一化事件模型（dev_spec 9.7.1）。
//
// Provider 适配器只负责把各自的报文映射到这里；C7/C8/C9 不接触原始协议。
package agentevent

import "encoding/json"

// Kind 是归一化事件类型。
type Kind string

const (
	KindStreamDelta  Kind = "stream_delta"
	KindThoughtDelta Kind = "thought_delta"
	KindToolCall     Kind = "tool_call"
	KindToolUpdate   Kind = "tool_update"
	KindPlan         Kind = "plan"
	KindUsage        Kind = "usage"
	KindNotice       Kind = "notice"
)

// Event 是一条归一化事件。
type Event struct {
	SessionID  string          `json:"session_id"`
	Kind       Kind            `json:"kind"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Title      string          `json:"title,omitempty"`
	ToolKind   string          `json:"tool_kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
}

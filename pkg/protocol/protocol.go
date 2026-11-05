// Package protocol 实现控制面 ↔ daemon 的 WS 信封（dev_spec 9.7）。
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Version 是当前 WS 协议版本。
const Version = "2.0"

// Type 是信封类型。
type Type string

const (
	TypeHandshake    Type = "handshake"
	TypeHandshakeAck Type = "handshake_ack"
	TypeHeartbeat    Type = "heartbeat"
	TypeCommand      Type = "command"
	TypeResponse     Type = "response"
	TypeEvent        Type = "event"
	TypeStream       Type = "stream"
)

var knownTypes = map[Type]bool{
	TypeHandshake:    true,
	TypeHandshakeAck: true,
	TypeHeartbeat:    true,
	TypeCommand:      true,
	TypeResponse:     true,
	TypeEvent:        true,
	TypeStream:       true,
}

// Envelope 是 9.7 定义的消息信封。
type Envelope struct {
	Version          string          `json:"version"`
	Type             Type            `json:"type"`
	ID               string          `json:"id"`
	TraceID          string          `json:"trace_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	ExecutionID      string          `json:"execution_id,omitempty"`
	ExecutionAttempt *uint32         `json:"execution_attempt,omitempty"`
	StepID           string          `json:"step_id,omitempty"`
	TS               time.Time       `json:"ts"`
	Stream           *StreamMeta     `json:"stream,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
}

// StreamMeta 是流式片段的元数据。
type StreamMeta struct {
	ReplyID string `json:"reply_id"`
	Seq     uint64 `json:"seq"`
	Final   bool   `json:"final"`
}

// CommandPayload 是 command 的 payload。
type CommandPayload struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// EventPayload 是 event 的 payload。
type EventPayload struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ResponsePayload 是 response 的 payload。
type ResponsePayload struct {
	Accepted bool            `json:"accepted"`
	Error    *ResponseError  `json:"error,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// ResponseError 是 daemon 侧的错误原因（与 HTTP 错误码体系分离）。
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StreamPayload 是 stream 的 payload。
type StreamPayload struct {
	Delta string `json:"delta"`
}

// IsExecutionClass 判断消息是否必须携带围栏字段（9.7）。
func (e Envelope) IsExecutionClass() bool {
	switch e.Type {
	case TypeStream:
		return true
	case TypeCommand:
		var p CommandPayload
		return json.Unmarshal(e.Payload, &p) == nil && p.Method == "start_step_session"
	case TypeEvent:
		var p EventPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			return false
		}
		return p.Kind == "step_done" || p.Kind == "step_fail"
	case TypeResponse:
		return e.ExecutionID != ""
	default:
		return false
	}
}

// Validate 校验信封（B7 验收：未知 type 拒绝；执行类缺 execution_attempt 拒绝）。
func (e Envelope) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("protocol: 不支持的版本 %q（期望 %q）", e.Version, Version)
	}
	if !knownTypes[e.Type] {
		return fmt.Errorf("protocol: 未知消息类型 %q", e.Type)
	}
	if e.ID == "" {
		return fmt.Errorf("protocol: id 不能为空")
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return fmt.Errorf("protocol: payload 不是合法 JSON")
	}
	if e.Type == TypeStream {
		if e.Stream == nil || e.Stream.ReplyID == "" {
			return fmt.Errorf("protocol: stream 消息缺少 stream 元数据")
		}
	}
	if e.IsExecutionClass() {
		if e.ExecutionID == "" {
			return fmt.Errorf("protocol: 执行类消息缺少 execution_id")
		}
		if e.ExecutionAttempt == nil {
			return fmt.Errorf("protocol: 执行类消息缺少 execution_attempt")
		}
	}
	return nil
}

// Encode 补齐默认值并序列化；序列化前先校验。
func Encode(e Envelope) ([]byte, error) {
	if e.Version == "" {
		e.Version = Version
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if e.Type == TypeEvent {
		// H3：事件随日志/审计落库前脱敏；stream_delta 保持原文（代码内容）
		e.Payload = RedactJSON(e.Payload)
	}
	return json.Marshal(e)
}

// Decode 反序列化并校验；未知字段视为协议错误。
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return e, fmt.Errorf("protocol: 解码失败: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}

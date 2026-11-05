package daemon

import (
	"context"
	"encoding/json"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// Sender 让命令处理器在执行过程中回传事件与流。
type Sender interface {
	SendEvent(kind, executionID string, attempt uint32, stepID string, data any) error
	SendStream(executionID string, attempt uint32, stepID string, seq uint64, delta string, final bool) error
}

type senderCtxKey struct{}

// WithSender 把连接级 Sender 放进命令处理 context。
func WithSender(ctx context.Context, sender Sender) context.Context {
	return context.WithValue(ctx, senderCtxKey{}, sender)
}

// SenderFrom 取出 Sender。
func SenderFrom(ctx context.Context) (Sender, bool) {
	sender, ok := ctx.Value(senderCtxKey{}).(Sender)
	return sender, ok
}

type connSender struct {
	writer  *envelopeWriter
	traceID string
}

func (s *connSender) envelope(executionID string, attempt uint32, stepID string) protocol.Envelope {
	env := protocol.Envelope{
		ID:          idgen.New(),
		TraceID:     s.traceID,
		ExecutionID: executionID,
		StepID:      stepID,
	}
	if attempt > 0 {
		a := attempt
		env.ExecutionAttempt = &a
	}
	return env
}

// SendEvent 发送一条归一化事件（step_done / step_fail / agent_ready 等）。
func (s *connSender) SendEvent(kind, executionID string, attempt uint32, stepID string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(protocol.EventPayload{Kind: kind, Data: raw})
	if err != nil {
		return err
	}
	env := s.envelope(executionID, attempt, stepID)
	env.Type = protocol.TypeEvent
	env.Payload = payload
	return s.writer.send(env)
}

// SendStream 发送一段流式内容。
func (s *connSender) SendStream(executionID string, attempt uint32, stepID string, seq uint64, delta string, final bool) error {
	payload, err := json.Marshal(protocol.StreamPayload{Delta: delta})
	if err != nil {
		return err
	}
	env := s.envelope(executionID, attempt, stepID)
	env.Type = protocol.TypeStream
	env.Stream = &protocol.StreamMeta{ReplyID: executionID, Seq: seq, Final: final}
	env.Payload = payload
	return s.writer.send(env)
}

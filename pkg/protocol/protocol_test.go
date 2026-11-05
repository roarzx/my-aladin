package protocol

import (
	"encoding/json"
	"testing"
)

func attempt(n uint32) *uint32 { return &n }

func mustPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	payload := mustPayload(t, CommandPayload{
		Method: "start_planning_session",
		Params: json.RawMessage(`{"task_id":"t1"}`),
	})
	data, err := Encode(Envelope{
		Type: TypeCommand, ID: "1", TraceID: "tr", SessionID: "s1", Payload: payload,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Version != Version || got.Type != TypeCommand || got.ID != "1" {
		t.Fatalf("信封 = %+v", got)
	}
	if got.TS.IsZero() {
		t.Fatal("ts 应被补齐")
	}
}

func TestUnknownTypeRejected(t *testing.T) {
	if _, err := Encode(Envelope{Type: Type("bogus"), ID: "1"}); err == nil {
		t.Fatal("未知 type 应被拒绝")
	}
}

func TestWrongVersionRejected(t *testing.T) {
	if _, err := Encode(Envelope{Version: "1.0", Type: TypeHeartbeat, ID: "1"}); err == nil {
		t.Fatal("错误版本应被拒绝")
	}
}

func TestExecutionFence(t *testing.T) {
	cases := []struct {
		name    string
		env     Envelope
		wantErr bool
	}{
		{
			name: "stream 缺 exec",
			env: Envelope{Type: TypeStream, ID: "1",
				Stream: &StreamMeta{ReplyID: "r", Seq: 1}},
			wantErr: true,
		},
		{
			name: "stream 带 exec",
			env: Envelope{Type: TypeStream, ID: "1", ExecutionID: "e", ExecutionAttempt: attempt(1),
				Stream: &StreamMeta{ReplyID: "r", Seq: 1}},
		},
		{
			name: "start_step_session 缺 exec",
			env: Envelope{Type: TypeCommand, ID: "1",
				Payload: mustPayload(t, CommandPayload{Method: "start_step_session"})},
			wantErr: true,
		},
		{
			name: "start_step_session 带 exec",
			env: Envelope{Type: TypeCommand, ID: "1", ExecutionID: "e", ExecutionAttempt: attempt(2),
				Payload: mustPayload(t, CommandPayload{Method: "start_step_session"})},
		},
		{
			name: "start_planning_session 不需要 exec",
			env: Envelope{Type: TypeCommand, ID: "1",
				Payload: mustPayload(t, CommandPayload{Method: "start_planning_session"})},
		},
		{
			name: "step_done 缺 exec",
			env: Envelope{Type: TypeEvent, ID: "1",
				Payload: mustPayload(t, EventPayload{Kind: "step_done"})},
			wantErr: true,
		},
		{
			name: "step_fail 带 exec",
			env: Envelope{Type: TypeEvent, ID: "1", ExecutionID: "e", ExecutionAttempt: attempt(1),
				Payload: mustPayload(t, EventPayload{Kind: "step_fail"})},
		},
		{
			name: "普通 event 不需要 exec",
			env: Envelope{Type: TypeEvent, ID: "1",
				Payload: mustPayload(t, EventPayload{Kind: "agent_ready"})},
		},
		{
			name:    "response 带 execution_id 但缺 attempt",
			env:     Envelope{Type: TypeResponse, ID: "1", ExecutionID: "e"},
			wantErr: true,
		},
		{
			name: "普通 response",
			env: Envelope{Type: TypeResponse, ID: "1",
				Payload: mustPayload(t, ResponsePayload{Accepted: true})},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(tc.env)
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望校验失败")
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过: %v", err)
			}
		})
	}
}

func TestStreamRequiresMeta(t *testing.T) {
	env := Envelope{Type: TypeStream, ID: "1", ExecutionID: "e", ExecutionAttempt: attempt(1)}
	if _, err := Encode(env); err == nil {
		t.Fatal("stream 缺少 stream 元数据应被拒绝")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	raw := `{"version":"2.0","type":"heartbeat","id":"1","typo":1,"ts":"2026-01-01T00:00:00Z"}`
	if _, err := Decode([]byte(raw)); err == nil {
		t.Fatal("未知字段应被拒绝")
	}
}

func TestEncodeRejectsInvalidPayload(t *testing.T) {
	env := Envelope{Type: TypeEvent, ID: "1", Payload: json.RawMessage(`{not json`)}
	if _, err := Encode(env); err == nil {
		t.Fatal("非法 payload 应被拒绝")
	}
}

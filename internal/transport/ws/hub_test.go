package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/runtime"
	"github.com/roarzx/my-aladin/internal/session"
	"github.com/roarzx/my-aladin/pkg/errcode"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

func waitRegistered(t *testing.T, hub *Hub, machineID string) *Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if wsSession, err := hub.session(machineID); err == nil {
			return wsSession
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("会话未注册")
	return nil
}

func respond(conn *fakeConn, id string, accepted bool, code, message string) {
	payload := protocol.ResponsePayload{Accepted: accepted}
	if !accepted {
		payload.Error = &protocol.ResponseError{Code: code, Message: message}
	}
	data, _ := json.Marshal(payload)
	conn.in <- protocol.Envelope{Type: protocol.TypeResponse, ID: id, Payload: data}
}

func TestHubDispatchStepSession(t *testing.T) {
	hub := NewHub(nil)
	conn := newFakeConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.serve(ctx, "m1", conn) }()
	waitRegistered(t, hub, "m1")

	go func() {
		cmd := <-conn.out
		if cmd.Type != protocol.TypeCommand || cmd.ExecutionID != "exec-1" ||
			cmd.ExecutionAttempt == nil || *cmd.ExecutionAttempt != 2 {
			t.Errorf("命令信封 = %+v", cmd)
		}
		var payload protocol.CommandPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Method != "start_step_session" {
			t.Errorf("payload = %s, err=%v", cmd.Payload, err)
		}
		respond(conn, cmd.ID, true, "", "")
	}()

	id, err := hub.Dispatch(ctx, runtime.DispatchRequest{
		ExecutionID: "exec-1", MachineID: "m1", TaskID: "t1", StepID: "s1", AttemptNo: 2,
	})
	if err != nil || id != "exec-1" {
		t.Fatalf("Dispatch = %q, err=%v", id, err)
	}
}

func TestHubDispatchRejected(t *testing.T) {
	hub := NewHub(nil)
	conn := newFakeConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.serve(ctx, "m1", conn) }()
	waitRegistered(t, hub, "m1")

	go func() {
		cmd := <-conn.out
		respond(conn, cmd.ID, false, "runtime_start_failed", "not running")
	}()
	_, err := hub.Dispatch(ctx, runtime.DispatchRequest{
		ExecutionID: "exec-2", MachineID: "m1", AttemptNo: 1,
	})
	e, ok := errcode.As(err)
	if !ok || e.Code != errcode.RuntimeStartFailed {
		t.Fatalf("错误码 = %v", err)
	}
}

func TestHubDispatchOffline(t *testing.T) {
	hub := NewHub(nil)
	_, err := hub.Dispatch(context.Background(), runtime.DispatchRequest{
		ExecutionID: "exec-3", MachineID: "missing", AttemptNo: 1,
	})
	if !errors.Is(err, ErrMachineOffline) {
		t.Fatalf("期望 ErrMachineOffline，实际: %v", err)
	}
}

func TestHubDispatchPlanning(t *testing.T) {
	hub := NewHub(nil)
	conn := newFakeConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.serve(ctx, "m1", conn) }()
	waitRegistered(t, hub, "m1")

	go func() {
		cmd := <-conn.out
		var payload protocol.CommandPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Method != "start_planning_session" {
			t.Errorf("payload = %s, err=%v", cmd.Payload, err)
		}
		respond(conn, cmd.ID, true, "", "")
	}()

	if err := hub.DispatchPlanning(ctx, session.PlanningDispatch{
		TaskID: "t1", SessionID: "s1", AgentID: "a1", MachineID: "m1", Prompt: "p",
	}); err != nil {
		t.Fatalf("DispatchPlanning: %v", err)
	}
}

func TestHubCancelUsesExecutionMachine(t *testing.T) {
	hub := NewHub(nil)
	conn := newFakeConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.serve(ctx, "m1", conn) }()
	waitRegistered(t, hub, "m1")

	go func() {
		cmd := <-conn.out
		respond(conn, cmd.ID, true, "", "")
	}()
	if _, err := hub.Dispatch(ctx, runtime.DispatchRequest{
		ExecutionID: "exec-4", MachineID: "m1", AttemptNo: 1,
	}); err != nil {
		t.Fatal(err)
	}

	go func() {
		cmd := <-conn.out
		var payload protocol.CommandPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Method != "stop_session" {
			t.Errorf("payload = %s, err=%v", cmd.Payload, err)
		}
		respond(conn, cmd.ID, true, "", "")
	}()
	acked, err := hub.Cancel(ctx, "exec-4")
	if err != nil || !acked {
		t.Fatalf("Cancel = %v, err=%v", acked, err)
	}
}

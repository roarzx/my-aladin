package fakeruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/roarzx/my-aladin/internal/runtime"
)

func loadFixture(t *testing.T) *Script {
	t.Helper()
	script, err := Load("../../../tests/fixtures/scripts/basic.yaml")
	if err != nil {
		t.Fatalf("加载 fixture: %v", err)
	}
	return script
}

func TestDispatchCompleted(t *testing.T) {
	rt := New(loadFixture(t))
	var events []Event
	rt.OnEvent = func(e Event) { events = append(events, e) }

	id, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{ExecutionID: "exec-ok"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "exec-ok" {
		t.Fatalf("executionID = %q", id)
	}
	if len(events) != 2 || events[0].Kind != "stream_delta" || events[1].Kind != "tool_call" {
		t.Fatalf("事件 = %+v", events)
	}
	snap, err := rt.Inspect(context.Background(), id)
	if err != nil || snap.State != runtime.StateCompleted {
		t.Fatalf("快照 = %+v, err = %v", snap, err)
	}
}

func TestDispatchFailure(t *testing.T) {
	rt := New(loadFixture(t))
	id, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{ExecutionID: "exec-fail"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	snap, err := rt.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != runtime.StateFailed || snap.ErrorClass != "tool" {
		t.Fatalf("快照 = %+v", snap)
	}
}

func TestDispatchDisconnect(t *testing.T) {
	rt := New(loadFixture(t))
	_, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{ExecutionID: "exec-dispatch-error"})
	if !errors.Is(err, ErrDispatch) {
		t.Fatalf("期望 ErrDispatch，实际: %v", err)
	}
}

func TestHangThenCancel(t *testing.T) {
	rt := New(loadFixture(t))
	ctx := context.Background()
	id, err := rt.Dispatch(ctx, runtime.DispatchRequest{ExecutionID: "exec-hang"})
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := rt.Inspect(ctx, id)
	if snap.State != runtime.StateRunning {
		t.Fatalf("hang 应保持 running，实际 %q", snap.State)
	}
	acked, err := rt.Cancel(ctx, id)
	if err != nil || !acked {
		t.Fatalf("取消应成功: acked=%v err=%v", acked, err)
	}
	snap, _ = rt.Inspect(ctx, id)
	if snap.State != runtime.StateCancelled {
		t.Fatalf("取消后状态 = %q", snap.State)
	}
	if acked, _ := rt.Cancel(ctx, id); acked {
		t.Fatal("重复取消不应再次确认")
	}
}

func TestCapabilities(t *testing.T) {
	rt := New(nil)
	if rt.Capabilities().Git {
		t.Fatal("默认不应声明能力")
	}
	rt.SetCapabilities(runtime.CapabilitySet{Git: true})
	if !rt.Capabilities().Git {
		t.Fatal("能力未生效")
	}
}

func TestUnknownExecution(t *testing.T) {
	rt := New(nil)
	if _, err := rt.Inspect(context.Background(), "nope"); err == nil {
		t.Fatal("未知执行应报错")
	}
	if _, err := rt.Cancel(context.Background(), "nope"); err == nil {
		t.Fatal("未知执行取消应报错")
	}
	if _, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{ExecutionID: "nope"}); err == nil {
		t.Fatal("无脚本时派发应报错")
	}
}

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

func TestDispatchReplaysEvents(t *testing.T) {
	rt := New(loadFixture(t))
	var events []Event
	rt.OnEvent = func(e Event) { events = append(events, e) }

	id, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{
		ExecutionID: "exec-ok", IssueID: "issue-1",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "exec-ok" {
		t.Fatalf("executionID = %q", id)
	}
	if len(events) != 2 || events[0].Kind != "stream_delta" || events[1].Kind != "tool_call" {
		t.Fatalf("事件 = %+v", events)
	}
	dispatched := rt.Dispatched()
	if len(dispatched) != 1 || dispatched[0].IssueID != "issue-1" {
		t.Fatalf("派发记录 = %+v", dispatched)
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
	if _, err := rt.Dispatch(ctx, runtime.DispatchRequest{ExecutionID: "exec-hang"}); err != nil {
		t.Fatal(err)
	}
	acked, err := rt.Cancel(ctx, "exec-hang")
	if err != nil || !acked {
		t.Fatalf("取消应成功: acked=%v err=%v", acked, err)
	}
	if acked, _ := rt.Cancel(ctx, "exec-hang"); !acked {
		t.Fatal("取消是幂等的")
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
	if _, err := rt.Dispatch(context.Background(), runtime.DispatchRequest{ExecutionID: "nope"}); err == nil {
		t.Fatal("无脚本时派发应报错")
	}
}

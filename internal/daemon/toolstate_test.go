package daemon

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
)

func TestToolPairing10k(t *testing.T) {
	state := NewToolState()
	const n = 10000
	for i := 0; i < n; i++ {
		id := "call-" + strconv.Itoa(i)
		state.StartToolCall(id, "tool", "read")
		if !state.UpdateToolCall(id, "completed", json.RawMessage(`{"ok":true}`)) {
			t.Fatalf("更新失败: %s", id)
		}
	}
	if open := state.Open(); len(open) != 0 {
		t.Fatalf("仍有 %d 个未闭合调用", len(open))
	}
	opened, closed := state.Stats()
	if opened != n || closed != n {
		t.Fatalf("opened=%d closed=%d", opened, closed)
	}
}

func TestToolOpenAfterInterruption(t *testing.T) {
	state := NewToolState()
	state.StartToolCall("a", "first", "read")
	state.StartToolCall("b", "second", "write")
	state.UpdateToolCall("a", "completed", nil)

	open := state.Open()
	if len(open) != 1 || open[0].ID != "b" || open[0].Status != "pending" {
		t.Fatalf("未闭合调用 = %+v", open)
	}

	if state.UpdateToolCall("missing", "completed", nil) {
		t.Fatal("未知 id 不应更新成功")
	}
}

func TestToolConcurrentUpdates(t *testing.T) {
	state := NewToolState()
	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "c-" + strconv.Itoa(i)
			state.StartToolCall(id, "t", "read")
			state.UpdateToolCall(id, "failed", json.RawMessage(`{"err":"x"}`))
		}(i)
	}
	wg.Wait()
	if open := state.Open(); len(open) != 0 {
		t.Fatalf("未闭合 = %d", len(open))
	}
	opened, closed := state.Stats()
	if opened != n || closed != n {
		t.Fatalf("opened=%d closed=%d", opened, closed)
	}
}

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/pkg/protocol"
)

func commandEnvelope(id string) protocol.Envelope {
	payload, _ := json.Marshal(protocol.CommandPayload{Method: "ping"})
	return protocol.Envelope{Type: protocol.TypeCommand, ID: id, Payload: payload}
}

func TestDispatcherDeduplicates(t *testing.T) {
	store, err := OpenIdempotentStore(filepath.Join(t.TempDir(), "commands.jsonl"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var calls atomic.Int32
	handler := func(context.Context, protocol.Envelope) (protocol.ResponsePayload, error) {
		calls.Add(1)
		return protocol.ResponsePayload{Accepted: true, Data: json.RawMessage(`"ok"`)}, nil
	}
	d := NewDispatcher(store, handler, nil)

	env := commandEnvelope("cmd-1")
	for i := 0; i < 3; i++ {
		payload, err := d.Handle(context.Background(), env)
		if err != nil || !payload.Accepted {
			t.Fatalf("第 %d 次: %+v, err=%v", i, payload, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler 调用 %d 次，期望 1", calls.Load())
	}
}

func TestDispatcherConcurrentSameID(t *testing.T) {
	store, err := OpenIdempotentStore(filepath.Join(t.TempDir(), "commands.jsonl"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var calls atomic.Int32
	handler := func(context.Context, protocol.Envelope) (protocol.ResponsePayload, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return protocol.ResponsePayload{Accepted: true}, nil
	}
	d := NewDispatcher(store, handler, nil)
	env := commandEnvelope("cmd-race")

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.Handle(context.Background(), env); err != nil {
				t.Errorf("Handle: %v", err)
			}
		}()
	}
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("并发同 id handler 调用 %d 次，期望 1", calls.Load())
	}
}

func TestDispatcherDoesNotCacheErrors(t *testing.T) {
	store, err := OpenIdempotentStore(filepath.Join(t.TempDir(), "commands.jsonl"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var calls atomic.Int32
	handler := func(context.Context, protocol.Envelope) (protocol.ResponsePayload, error) {
		if calls.Add(1) == 1 {
			return protocol.ResponsePayload{}, errors.New("temporary")
		}
		return protocol.ResponsePayload{Accepted: true}, nil
	}
	d := NewDispatcher(store, handler, nil)
	env := commandEnvelope("cmd-retry")

	if _, err := d.Handle(context.Background(), env); err == nil {
		t.Fatal("首次应返回错误")
	}
	payload, err := d.Handle(context.Background(), env)
	if err != nil || !payload.Accepted {
		t.Fatalf("第二次应成功: %+v, err=%v", payload, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestDispatcherSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	store1, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	calls := atomic.Int32{}
	handler := func(context.Context, protocol.Envelope) (protocol.ResponsePayload, error) {
		calls.Add(1)
		return protocol.ResponsePayload{Accepted: true}, nil
	}
	if _, err := NewDispatcher(store1, handler, nil).Handle(context.Background(), commandEnvelope("cmd-1")); err != nil {
		t.Fatal(err)
	}
	_ = store1.Close()

	store2, err := OpenIdempotentStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	payload, err := NewDispatcher(store2, handler, nil).Handle(context.Background(), commandEnvelope("cmd-1"))
	if err != nil || !payload.Accepted {
		t.Fatalf("重启后应命中缓存: %+v, err=%v", payload, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("重启后不应再次执行，calls = %d", calls.Load())
	}
}

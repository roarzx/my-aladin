package ws

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/unacked"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

type fakeStore struct {
	mu      sync.Mutex
	entries map[string]unacked.Entry
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: make(map[string]unacked.Entry)}
}

func (f *fakeStore) Track(_ context.Context, _, id string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[id] = unacked.Entry{ID: id, Payload: payload}
	return nil
}

func (f *fakeStore) Ack(_ context.Context, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, id)
	return nil
}

func (f *fakeStore) Due(_ context.Context, _ string, _ time.Time, _ int) ([]unacked.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]unacked.Entry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func encodeEvent(t *testing.T, id string) []byte {
	t.Helper()
	data, err := protocol.Encode(protocol.Envelope{
		Type:    protocol.TypeEvent,
		ID:      id,
		Payload: json.RawMessage(`{"kind":"agent_ready"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestResendLoopKeepsSameID(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()
	_ = store.Track(ctx, "s1", "e1", encodeEvent(t, "e1"))

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := make(chan []byte, 16)
	go ResendLoop(runCtx, store, "s1", func(p []byte) error {
		sent <- p
		return nil
	}, 10*time.Millisecond, time.Millisecond, 10)

	for i := 0; i < 3; i++ {
		select {
		case data := <-sent:
			env, err := protocol.Decode(data)
			if err != nil || env.ID != "e1" {
				t.Fatalf("重投消息 id 变化: %+v err=%v", env, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("未发生重投")
		}
	}
}

func TestAckStopsResend(t *testing.T) {
	store := newFakeStore()
	ctx := context.Background()
	_ = store.Track(ctx, "s1", "e1", encodeEvent(t, "e1"))
	_ = store.Ack(ctx, "s1", "e1")

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := make(chan []byte, 4)
	go ResendLoop(runCtx, store, "s1", func(p []byte) error {
		sent <- p
		return nil
	}, 10*time.Millisecond, time.Millisecond, 10)

	select {
	case <-sent:
		t.Fatal("已 ack 的消息不应重投")
	case <-time.After(80 * time.Millisecond):
	}
}

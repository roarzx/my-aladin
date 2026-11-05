package ws

import (
	"context"
	"testing"
	"time"
)

type fakeBPStore struct {
	pending     int64
	pausedSince int64
}

func (f *fakeBPStore) Add(_ context.Context, _ string, delta int64, _ time.Duration) (int64, error) {
	f.pending += delta
	return f.pending, nil
}

func (f *fakeBPStore) State(context.Context, string) (int64, int64, error) {
	return f.pending, f.pausedSince, nil
}

func (f *fakeBPStore) SetPausedSince(_ context.Context, _ string, at int64, _ time.Duration) error {
	f.pausedSince = at
	return nil
}

func (f *fakeBPStore) ClearPausedSince(context.Context, string) error {
	f.pausedSince = 0
	return nil
}

func TestBackpressure(t *testing.T) {
	ctx := context.Background()
	store := &fakeBPStore{}
	now := time.Unix(1000, 0)
	bp := NewBackpressure(store, BackpressureConfig{
		High: 100, Low: 25, Max: 400, MaxPause: 30 * time.Second,
	})
	bp.Now = func() time.Time { return now }

	// 低于高水位 → continue
	if action, err := bp.Add(ctx, "s1", 50); err != nil || action != BPActionContinue {
		t.Fatalf("低水位 action=%v err=%v", action, err)
	}

	// 超过高水位 → pause（记录暂停时刻）
	if action, _ := bp.Add(ctx, "s1", 80); action != BPActionPause {
		t.Fatalf("高水位 action=%v", action)
	}
	if store.pausedSince == 0 {
		t.Fatal("应记录暂停时刻")
	}

	// 持续超阈值超过 MaxPause → terminate
	now = now.Add(31 * time.Second)
	if action, _ := bp.Check(ctx, "s1"); action != BPActionTerminate {
		t.Fatalf("持续背压 action=%v", action)
	}

	// 硬上限 → 立即 terminate
	store.pending, store.pausedSince = 500, 0
	if action, _ := bp.Check(ctx, "s1"); action != BPActionTerminate {
		t.Fatalf("硬上限 action=%v", action)
	}

	// 消费到低水位以下 → 恢复 continue 并清除暂停标记
	store.pending, store.pausedSince = 101, now.UnixNano()
	if action, _ := bp.Check(ctx, "s1"); action != BPActionPause {
		t.Fatalf("迟滞区间应保持 pause，实际 %v", action)
	}
	if action, _ := bp.Ack(ctx, "s1", 90); action != BPActionContinue {
		t.Fatalf("恢复 action=%v", action)
	}
	if store.pausedSince != 0 {
		t.Fatal("恢复后应清除暂停标记")
	}
}

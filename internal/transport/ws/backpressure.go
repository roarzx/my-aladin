package ws

import (
	"context"
	"time"
)

// BackpressureAction 是背压判定结果（E8）。
type BackpressureAction int

const (
	BPActionContinue BackpressureAction = iota
	BPActionPause
	BPActionTerminate
)

func (a BackpressureAction) String() string {
	switch a {
	case BPActionPause:
		return "pause"
	case BPActionTerminate:
		return "terminate"
	default:
		return "continue"
	}
}

// BackpressureStore 是会话级待处理字节计数（Redis 键 bp:{session_id}）。
type BackpressureStore interface {
	Add(ctx context.Context, sessionID string, delta int64, ttl time.Duration) (int64, error)
	State(ctx context.Context, sessionID string) (pending int64, pausedSinceUnixNano int64, err error)
	SetPausedSince(ctx context.Context, sessionID string, atUnixNano int64, ttl time.Duration) error
	ClearPausedSince(ctx context.Context, sessionID string) error
}

// BackpressureConfig 是水位配置。
type BackpressureConfig struct {
	High     int64
	Low      int64
	Max      int64
	MaxPause time.Duration
	TTL      time.Duration
}

func (c BackpressureConfig) withDefaults() BackpressureConfig {
	if c.High <= 0 {
		c.High = 8 << 20
	}
	if c.Low <= 0 {
		c.Low = c.High / 4
	}
	if c.Max <= 0 {
		c.Max = 4 * c.High
	}
	if c.MaxPause <= 0 {
		c.MaxPause = 30 * time.Second
	}
	if c.TTL <= 0 {
		c.TTL = 60 * time.Second
	}
	return c
}

// Backpressure 实现“消费变慢 → 暂停；持续超阈值 → 终止”（E8）。
type Backpressure struct {
	Store  BackpressureStore
	Config BackpressureConfig
	Now    func() time.Time
}

// NewBackpressure 构造背压监视器。
func NewBackpressure(store BackpressureStore, cfg BackpressureConfig) *Backpressure {
	return &Backpressure{Store: store, Config: cfg.withDefaults(), Now: time.Now}
}

// Add 记录新增待处理字节并返回当前动作。
func (b *Backpressure) Add(ctx context.Context, sessionID string, delta int64) (BackpressureAction, error) {
	if _, err := b.Store.Add(ctx, sessionID, delta, b.Config.TTL); err != nil {
		return BPActionContinue, err
	}
	return b.Check(ctx, sessionID)
}

// Ack 记录已消费字节并返回当前动作。
func (b *Backpressure) Ack(ctx context.Context, sessionID string, delta int64) (BackpressureAction, error) {
	if _, err := b.Store.Add(ctx, sessionID, -delta, b.Config.TTL); err != nil {
		return BPActionContinue, err
	}
	return b.Check(ctx, sessionID)
}

// Check 按水位判定动作；暂停标记带迟滞，低于 Low 才恢复。
func (b *Backpressure) Check(ctx context.Context, sessionID string) (BackpressureAction, error) {
	pending, pausedSince, err := b.Store.State(ctx, sessionID)
	if err != nil {
		return BPActionContinue, err
	}
	if pending > b.Config.Max {
		return BPActionTerminate, nil
	}
	if pending > b.Config.High {
		now := b.Now()
		if pausedSince == 0 {
			if err := b.Store.SetPausedSince(ctx, sessionID, now.UnixNano(), b.Config.TTL); err != nil {
				return BPActionContinue, err
			}
			return BPActionPause, nil
		}
		if now.Sub(time.Unix(0, pausedSince)) > b.Config.MaxPause {
			return BPActionTerminate, nil
		}
		return BPActionPause, nil
	}
	if pausedSince != 0 && pending > b.Config.Low {
		return BPActionPause, nil
	}
	if pausedSince != 0 {
		if err := b.Store.ClearPausedSince(ctx, sessionID); err != nil {
			return BPActionContinue, err
		}
	}
	return BPActionContinue, nil
}

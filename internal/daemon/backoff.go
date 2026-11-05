package daemon

import (
	"math/rand/v2"
	"time"
)

// Backoff 计算重连等待时间（指数退避 + 抖动）。
type Backoff struct {
	Base time.Duration
	Max  time.Duration
}

// Wait 返回第 attempt 次重连前的等待时间。
func (b Backoff) Wait(attempt int) time.Duration {
	base := b.Base
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	maxWait := b.Max
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 10 {
		attempt = 10
	}

	d := base << attempt
	if d > maxWait {
		d = maxWait
	}
	// ±10% 抖动，避免多台机器同时重连
	jitter := time.Duration(rand.Int64N(int64(d/5)+1)) - d/10
	if d+jitter < 0 {
		return 0
	}
	return d + jitter
}

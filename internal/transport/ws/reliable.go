package ws

import (
	"context"
	"time"

	"github.com/roarzx/my-aladin/internal/unacked"
)

// ResendLoop 周期性重投超过阈值仍未确认的消息（B9）。
//
// 重投使用 Track 时保存的原始字节，因此 message id 不变；接收端按 id 去重。
func ResendLoop(
	ctx context.Context,
	store unacked.Store,
	sessionID string,
	send func(payload []byte) error,
	interval, threshold time.Duration,
	limit int,
) {
	if interval <= 0 {
		interval = time.Second
	}
	if threshold <= 0 {
		threshold = 5 * time.Second
	}
	if limit <= 0 {
		limit = 100
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			entries, err := store.Due(ctx, sessionID, now.Add(-threshold), limit)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if err := send(entry.Payload); err != nil {
					break
				}
			}
		}
	}
}

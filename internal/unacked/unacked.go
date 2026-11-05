// Package unacked 定义未确认消息的存储接口（B9）。
//
// 传输层依赖本包，Redis 实现位于 internal/store/redis。
package unacked

import (
	"context"
	"time"
)

// Entry 是一条待重投的消息；Payload 是原始编码字节，重投时 id 不变。
type Entry struct {
	ID      string
	Payload []byte
}

// Store 记录已发送但未确认的消息。
type Store interface {
	Track(ctx context.Context, sessionID, id string, payload []byte) error
	Ack(ctx context.Context, sessionID, id string) error
	Due(ctx context.Context, sessionID string, olderThan time.Time, limit int) ([]Entry, error)
}

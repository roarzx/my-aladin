package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/roarzx/my-aladin/internal/unacked"
)

// UnackedStore 是 unacked.Store 的 Redis 实现（B9）。
type UnackedStore struct {
	client redis.UniversalClient
}

// NewUnackedStore 构造存储。
func NewUnackedStore(client redis.UniversalClient) *UnackedStore {
	return &UnackedStore{client: client}
}

func unackedKeys(sessionID string) (hash, zset string) {
	return "unacked:" + sessionID, "unacked:at:" + sessionID
}

// Track 记录一条已发送未确认的消息。
func (u *UnackedStore) Track(ctx context.Context, sessionID, id string, payload []byte) error {
	hash, zset := unackedKeys(sessionID)
	_, err := u.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, hash, id, payload)
		p.ZAdd(ctx, zset, redis.Z{Score: float64(time.Now().UnixMilli()), Member: id})
		return nil
	})
	if err != nil {
		return fmt.Errorf("track unacked: %w", err)
	}
	return nil
}

// Ack 删除已确认的消息。
func (u *UnackedStore) Ack(ctx context.Context, sessionID, id string) error {
	hash, zset := unackedKeys(sessionID)
	_, err := u.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HDel(ctx, hash, id)
		p.ZRem(ctx, zset, id)
		return nil
	})
	if err != nil {
		return fmt.Errorf("ack unacked: %w", err)
	}
	return nil
}

// Due 返回首次发送时间早于 olderThan 的消息。
func (u *UnackedStore) Due(ctx context.Context, sessionID string, olderThan time.Time, limit int) ([]unacked.Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	hash, zset := unackedKeys(sessionID)
	ids, err := u.client.ZRangeByScore(ctx, zset, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%d", olderThan.UnixMilli()),
		Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("unacked due: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	values, err := u.client.HMGet(ctx, hash, ids...).Result()
	if err != nil {
		return nil, fmt.Errorf("unacked payload: %w", err)
	}
	entries := make([]unacked.Entry, 0, len(values))
	for i, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		entries = append(entries, unacked.Entry{ID: ids[i], Payload: []byte(s)})
	}
	return entries, nil
}

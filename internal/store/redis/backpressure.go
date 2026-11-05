package redisstore

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// BackpressureStore 用 Hash 保存会话待处理字节与暂停时刻（bp:{session_id}）。
type BackpressureStore struct {
	client redis.UniversalClient
}

// NewBackpressureStore 构造背压存储。
func NewBackpressureStore(client redis.UniversalClient) *BackpressureStore {
	return &BackpressureStore{client: client}
}

func (s *BackpressureStore) key(sessionID string) string {
	return (Keys{}).Backpressure(sessionID)
}

// Add 累加待处理字节并刷新 TTL。
func (s *BackpressureStore) Add(ctx context.Context, sessionID string, delta int64, ttl time.Duration) (int64, error) {
	key := s.key(sessionID)
	pipe := s.client.TxPipeline()
	incr := pipe.HIncrBy(ctx, key, "pending", delta)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// State 读取待处理字节与暂停时刻（0 表示未暂停）。
func (s *BackpressureStore) State(ctx context.Context, sessionID string) (int64, int64, error) {
	values, err := s.client.HGetAll(ctx, s.key(sessionID)).Result()
	if err != nil {
		return 0, 0, err
	}
	pending, _ := strconv.ParseInt(values["pending"], 10, 64)
	pausedSince, _ := strconv.ParseInt(values["paused_since"], 10, 64)
	return pending, pausedSince, nil
}

// SetPausedSince 记录暂停时刻并刷新 TTL。
func (s *BackpressureStore) SetPausedSince(ctx context.Context, sessionID string, atUnixNano int64, ttl time.Duration) error {
	key := s.key(sessionID)
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, "paused_since", atUnixNano)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// ClearPausedSince 清除暂停标记。
func (s *BackpressureStore) ClearPausedSince(ctx context.Context, sessionID string) error {
	return s.client.HDel(ctx, s.key(sessionID), "paused_since").Err()
}

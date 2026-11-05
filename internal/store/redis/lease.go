package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// renewLeaseScript 仅在 epoch 匹配时续期，避免旧代次心跳恢复资格（dev_spec 9.8）。
var renewLeaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

// LeaseStore 是执行租约的 Redis 实现（键 lease:attempt:{id}，TTL 60s）。
type LeaseStore struct {
	client redis.UniversalClient
}

// NewLeaseStore 构造租约存储。
func NewLeaseStore(client redis.UniversalClient) *LeaseStore {
	return &LeaseStore{client: client}
}

// Acquire 仅在键不存在时写入 epoch。
func (l *LeaseStore) Acquire(ctx context.Context, attemptID string, epoch uint64, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("lease: ttl 必须大于 0")
	}
	return l.client.SetNX(ctx, (Keys{}).Lease(attemptID), epoch, ttl).Result()
}

// Renew 仅在 epoch 匹配时续期。
func (l *LeaseStore) Renew(ctx context.Context, attemptID string, epoch uint64, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("lease: ttl 必须大于 0")
	}
	n, err := renewLeaseScript.Run(ctx, l.client,
		[]string{(Keys{}).Lease(attemptID)}, epoch, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Exists 判断租约是否仍在。
func (l *LeaseStore) Exists(ctx context.Context, attemptID string) (bool, error) {
	n, err := l.client.Exists(ctx, (Keys{}).Lease(attemptID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete 删除租约。
func (l *LeaseStore) Delete(ctx context.Context, attemptID string) (bool, error) {
	n, err := l.client.Del(ctx, (Keys{}).Lease(attemptID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

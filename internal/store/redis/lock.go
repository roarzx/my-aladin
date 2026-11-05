package redisstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// 释放与续约都必须原子比较持有者（dev_spec 9.8：禁止无条件 DEL，
// 也禁止"先读再续期"的窗口）。
var (
	releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

	renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)
)

// Locker 是带持有者比较的 Redis 锁。
type Locker struct {
	client redis.UniversalClient
}

// NewLocker 构造 Locker。
func NewLocker(client redis.UniversalClient) *Locker {
	return &Locker{client: client}
}

// Acquire 尝试获取锁；成功返回 true。
func (l *Locker) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if owner == "" {
		return false, errors.New("lock: owner 不能为空")
	}
	if ttl <= 0 {
		return false, fmt.Errorf("lock: ttl 必须大于 0")
	}
	return l.client.SetNX(ctx, key, owner, ttl).Result()
}

// Renew 仅当持有者仍是 owner 时续期；返回是否续期成功。
func (l *Locker) Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("lock: ttl 必须大于 0")
	}
	n, err := renewScript.Run(ctx, l.client, []string{key}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Release 仅当持有者仍是 owner 时删除；返回是否删除成功。
func (l *Locker) Release(ctx context.Context, key, owner string) (bool, error) {
	n, err := releaseScript.Run(ctx, l.client, []string{key}, owner).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Package redisstore 封装 Redis 键、锁与租约（见 dev_spec 9.4 / 9.8）。
//
// 约定：键名只在本包生成，业务代码不得散落字符串字面量。
package redisstore

import "github.com/redis/go-redis/v9"

// New 创建一个指向 addr 的客户端。
func New(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}

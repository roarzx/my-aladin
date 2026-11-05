//go:build integration

package testkit

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// RedisAddr 返回集成测试使用的 Redis 地址，可通过 TEST_REDIS_ADDR 覆盖。
func RedisAddr() string {
	if v := os.Getenv("TEST_REDIS_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:6379"
}

// NewRedis 返回连到 15 号库的客户端；测试用键必须带唯一前缀，禁止 FLUSHDB。
func NewRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: RedisAddr(),
		DB:   15,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("Redis 不可达（检查 TEST_REDIS_ADDR=%s）: %v", RedisAddr(), err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

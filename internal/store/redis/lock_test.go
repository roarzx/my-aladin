//go:build integration

package redisstore_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func lockKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test:lock:%s:%d", t.Name(), time.Now().UnixNano())
}

func TestLockOwnerSemantics(t *testing.T) {
	ctx := context.Background()
	client := testkit.NewRedis(t)
	locker := redisstore.NewLocker(client)
	key := lockKey(t)

	ok, err := locker.Acquire(ctx, key, "owner-A", 5*time.Second)
	if err != nil || !ok {
		t.Fatalf("首次获取应成功: ok=%v err=%v", ok, err)
	}

	if ok, err := locker.Acquire(ctx, key, "owner-B", 5*time.Second); err != nil || ok {
		t.Fatalf("第二个持有者不应获取成功: ok=%v err=%v", ok, err)
	}
	if ok, err := locker.Renew(ctx, key, "owner-B", 5*time.Second); err != nil || ok {
		t.Fatalf("非持有者不应续约成功: ok=%v err=%v", ok, err)
	}
	if ok, err := locker.Release(ctx, key, "owner-B"); err != nil || ok {
		t.Fatalf("非持有者释放应失败: ok=%v err=%v", ok, err)
	}
	if n := client.Exists(ctx, key).Val(); n != 1 {
		t.Fatalf("非持有者释放后锁不应消失")
	}

	if ok, err := locker.Renew(ctx, key, "owner-A", 5*time.Second); err != nil || !ok {
		t.Fatalf("持有者续约应成功: ok=%v err=%v", ok, err)
	}
	if ok, err := locker.Release(ctx, key, "owner-A"); err != nil || !ok {
		t.Fatalf("持有者释放应成功: ok=%v err=%v", ok, err)
	}
	if n := client.Exists(ctx, key).Val(); n != 0 {
		t.Fatalf("释放后键应消失")
	}
}

func TestLockExpires(t *testing.T) {
	ctx := context.Background()
	client := testkit.NewRedis(t)
	locker := redisstore.NewLocker(client)
	key := lockKey(t)

	ok, err := locker.Acquire(ctx, key, "owner-A", 100*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("获取应成功: ok=%v err=%v", ok, err)
	}
	time.Sleep(250 * time.Millisecond)

	ok, err = locker.Acquire(ctx, key, "owner-B", 5*time.Second)
	if err != nil || !ok {
		t.Fatalf("TTL 到期后应可被他人获取: ok=%v err=%v", ok, err)
	}
	if ok, err := locker.Release(ctx, key, "owner-B"); err != nil || !ok {
		t.Fatalf("清理失败: ok=%v err=%v", ok, err)
	}
}

func TestLockRejectsInvalidArgs(t *testing.T) {
	locker := redisstore.NewLocker(testkit.NewRedis(t))
	ctx := context.Background()

	if _, err := locker.Acquire(ctx, "k", "", time.Second); err == nil {
		t.Fatal("空 owner 应报错")
	}
	if _, err := locker.Acquire(ctx, "k", "o", 0); err == nil {
		t.Fatal("零 TTL 应报错")
	}
}

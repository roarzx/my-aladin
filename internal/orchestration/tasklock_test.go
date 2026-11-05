//go:build integration

package orchestration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestTaskLockMutualExclusion(t *testing.T) {
	ctx := context.Background()
	client := testkit.NewRedis(t)
	lock := orchestration.NewTaskLock(client, 300*time.Millisecond, -1)
	taskID := fmt.Sprintf("lock-%d", time.Now().UnixNano())

	releaseA, err := lock.Acquire(ctx, taskID, "owner-a")
	if err != nil {
		t.Fatalf("获取锁: %v", err)
	}
	if _, err := lock.Acquire(ctx, taskID, "owner-b"); !errors.Is(err, orchestration.ErrTaskLocked) {
		t.Fatalf("第二个持有者应失败，实际: %v", err)
	}

	releaseA()
	releaseB, err := lock.Acquire(ctx, taskID, "owner-b")
	if err != nil {
		t.Fatalf("释放后应可获取: %v", err)
	}
	releaseB()
}

func TestTaskLockExpires(t *testing.T) {
	ctx := context.Background()
	client := testkit.NewRedis(t)
	lock := orchestration.NewTaskLock(client, 100*time.Millisecond, -1)
	taskID := fmt.Sprintf("lock-ttl-%d", time.Now().UnixNano())

	if _, err := lock.Acquire(ctx, taskID, "owner-a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)

	release, err := lock.Acquire(ctx, taskID, "owner-b")
	if err != nil {
		t.Fatalf("TTL 到期后应可接管: %v", err)
	}
	release()
}

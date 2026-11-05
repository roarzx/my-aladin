//go:build integration

package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestLease(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	client := testkit.NewRedis(t)
	lease := &orchestration.Lease{
		DB: f.db, Store: redisstore.NewLeaseStore(client), TTL: 300 * time.Millisecond,
	}
	runner := &orchestration.PlanRunner{
		DB: f.db, Runtime: &stubRuntime{}, Owner: "runner-lease", Lease: lease,
	}
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	attempt, err := mysqlstore.GetLatestAttempt(ctx, f.db, f.step1)
	if err != nil {
		t.Fatal(err)
	}

	// 当前 epoch 可续约；旧 epoch 被拒绝
	if err := lease.Renew(ctx, attempt.ID, attempt.LeaseEpoch); err != nil {
		t.Fatalf("续约失败: %v", err)
	}
	if err := lease.Renew(ctx, attempt.ID, attempt.LeaseEpoch+1); !errors.Is(err, orchestration.ErrLeaseLost) {
		t.Fatalf("旧 epoch 应被拒绝，实际: %v", err)
	}

	time.Sleep(400 * time.Millisecond)

	expired, err := lease.Expire(ctx)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	// 集成库可能残留其它测试的活跃 attempt；只断言本轮目标被扫描到
	found := false
	for _, item := range expired {
		if item.AttemptID == attempt.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("过期列表未包含目标尝试 = %+v", expired)
	}

	got, err := mysqlstore.GetAttemptByID(ctx, f.db, attempt.ID)
	if err != nil || got.Status != domain.AttemptUnknown {
		t.Fatalf("attempt 状态 = %q, err=%v", got.Status, err)
	}
	step, err := mysqlstore.GetStepByID(ctx, f.db, f.step1)
	if err != nil || step.Status != domain.StepReconciling {
		t.Fatalf("step 状态 = %q, err=%v", step.Status, err)
	}
}

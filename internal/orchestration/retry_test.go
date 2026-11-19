//go:build integration

package orchestration_test

import (
	"context"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/testkit"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

func TestRetryCreatesNewAttempt(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"attempt_no": 1, "reason": "boom", "error_class": "tool"}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_fail", stub.requests[0].ExecutionID, 1, failed)); err != nil {
		t.Fatal(err)
	}

	service := &orchestration.RetryService{DB: f.db, Runtime: stub, MaxAttempts: 3}
	attemptNo, err := service.Retry(ctx, f.step1, "再试一次")
	if err != nil || attemptNo != 2 {
		t.Fatalf("Retry = %d, err=%v", attemptNo, err)
	}

	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepExecuting {
		t.Fatalf("step1 = %q", steps[0].Status)
	}
	plan, _, _ := mysqlstore.GetPlanByID(ctx, f.db, f.planID)
	if plan.Status != domain.PlanExecuting {
		t.Fatalf("plan = %q", plan.Status)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskExecuting {
		t.Fatalf("task = %q", task.Status)
	}
	if stub.count() != 2 {
		t.Fatalf("重试应重新派发，实际 %d", stub.count())
	}
	attempt, err := mysqlstore.GetAttemptByExecution(ctx, f.db, stub.requests[1].ExecutionID)
	if err != nil || attempt.AttemptNo != 2 {
		t.Fatalf("attempt = %+v, err=%v", attempt, err)
	}

	// 重试派发必须与主路径一致：补全 prompt / 工作目录 / 系统提示词，
	// 否则 agent 会在空提示词下空跑并误报成功。
	retryReq := stub.requests[1]
	if retryReq.Prompt == "" {
		t.Fatal("重试派发缺少 Prompt")
	}
	if retryReq.TaskTitle != "t" || retryReq.StepTitle == "" {
		t.Fatalf("重试派发缺少标题: %+v", retryReq)
	}
	if retryReq.WorkDir != "/tmp/work" || retryReq.SystemPrompt != "p" {
		t.Fatalf("重试派发缺少 Agent 配置: work_dir=%q system_prompt=%q", retryReq.WorkDir, retryReq.SystemPrompt)
	}
	if retryReq.LeaseEpoch == 0 {
		t.Fatal("重试派发缺少 LeaseEpoch")
	}
}

// 回归：重试必须申请租约，否则 Watchdog 的租约扫描会立刻把新尝试判为过期。
func TestRetryAcquiresLease(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"attempt_no": 1, "reason": "boom", "error_class": "tool"}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_fail", stub.requests[0].ExecutionID, 1, failed)); err != nil {
		t.Fatal(err)
	}

	client := testkit.NewRedis(t)
	lease := &orchestration.Lease{
		DB: f.db, Store: redisstore.NewLeaseStore(client), TTL: 300 * time.Millisecond,
	}
	service := &orchestration.RetryService{DB: f.db, Runtime: stub, MaxAttempts: 3, Lease: lease}
	if _, err := service.Retry(ctx, f.step1, ""); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	attempt, err := mysqlstore.GetAttemptByExecution(ctx, f.db, stub.requests[1].ExecutionID)
	if err != nil {
		t.Fatal(err)
	}

	// 刚派发的租约仍在：不应被判过期（这正是线上任务被误暂停的路径）。
	expired, err := lease.Expire(ctx)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	for _, item := range expired {
		if item.AttemptID == attempt.ID {
			t.Fatal("新尝试的租约不应立即过期")
		}
	}

	time.Sleep(400 * time.Millisecond)
	expired, err = lease.Expire(ctx)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	found := false
	for _, item := range expired {
		if item.AttemptID == attempt.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("租约到期后应被扫描到: %+v", expired)
	}
}

func TestRetryRejectsNonFailedStep(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	service := &orchestration.RetryService{DB: f.db, MaxAttempts: 3}

	_, err := service.Retry(context.Background(), f.step1, "")
	e, ok := errcode.As(err)
	if !ok || e.Code != errcode.StepNotFailed {
		t.Fatalf("期望 STEP_NOT_FAILED，实际: %v", err)
	}
}

func TestRetryBudgetExceeded(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"attempt_no": 1, "reason": "boom", "error_class": "verification"}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_fail", stub.requests[0].ExecutionID, 1, failed)); err != nil {
		t.Fatal(err)
	}

	service := &orchestration.RetryService{DB: f.db, Runtime: stub, MaxAttempts: 1}
	_, err := service.Retry(ctx, f.step1, "")
	e, ok := errcode.As(err)
	if !ok || e.Code != errcode.RetryBudgetExceeded {
		t.Fatalf("期望 RETRY_BUDGET_EXCEEDED，实际: %v", err)
	}
}

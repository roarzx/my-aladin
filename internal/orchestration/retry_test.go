//go:build integration

package orchestration_test

import (
	"context"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
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

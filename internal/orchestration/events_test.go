//go:build integration

package orchestration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

func eventEnvelope(kind, executionID string, attempt uint32, data any) protocol.Envelope {
	raw, _ := json.Marshal(data)
	payload, _ := json.Marshal(protocol.EventPayload{Kind: kind, Data: raw})
	execAttempt := attempt
	return protocol.Envelope{
		Type: protocol.TypeEvent, ID: idgen.New(),
		ExecutionID: executionID, ExecutionAttempt: &execAttempt, Payload: payload,
	}
}

func TestStepDoneAdvancesPlan(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	done := map[string]any{"attempt_no": 1, "summary_digest": "digest", "commit_sha": "abc123"}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_done", stub.requests[0].ExecutionID, 1, done)); err != nil {
		t.Fatal(err)
	}

	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepCompleted {
		t.Fatalf("step1 = %q", steps[0].Status)
	}
	if stub.count() != 2 {
		t.Fatalf("完成后应派发第二步，实际 %d", stub.count())
	}

	if err := handler.Handle(ctx, "m1", eventEnvelope("step_done", stub.requests[1].ExecutionID, 1, done)); err != nil {
		t.Fatal(err)
	}
	plan, _, err := mysqlstore.GetPlanByID(ctx, f.db, f.planID)
	if err != nil || plan.Status != domain.PlanCompleted {
		t.Fatalf("plan = %+v, err=%v", plan, err)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskChatting {
		t.Fatalf("task = %q", task.Status)
	}
}

func TestStepFailPauses(t *testing.T) {
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

	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepFailed {
		t.Fatalf("step1 = %q", steps[0].Status)
	}
	plan, _, _ := mysqlstore.GetPlanByID(ctx, f.db, f.planID)
	if plan.Status != domain.PlanPaused {
		t.Fatalf("plan = %q", plan.Status)
	}
	task, _ := mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskPaused {
		t.Fatalf("task = %q", task.Status)
	}
	attempt, _ := mysqlstore.GetAttemptByExecution(ctx, f.db, stub.requests[0].ExecutionID)
	if attempt.Status != domain.AttemptFailed || attempt.ErrorClass != domain.ErrTool {
		t.Fatalf("attempt = %+v", attempt)
	}
}

func TestStepFailUncertainGoesReconciling(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	uncertain := map[string]any{"attempt_no": 1, "reason": "timeout", "error_class": "network", "uncertain": true}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_fail", stub.requests[0].ExecutionID, 1, uncertain)); err != nil {
		t.Fatal(err)
	}
	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepReconciling {
		t.Fatalf("step1 = %q", steps[0].Status)
	}
	attempt, _ := mysqlstore.GetAttemptByExecution(ctx, f.db, stub.requests[0].ExecutionID)
	if attempt.Status != domain.AttemptUnknown {
		t.Fatalf("attempt = %q", attempt.Status)
	}
}

func TestLateStepDoneRejected(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	handler := &orchestration.EventHandler{DB: f.db, Runner: runner}

	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	done := map[string]any{"attempt_no": 9, "summary_digest": "d", "commit_sha": "c"}
	if err := handler.Handle(ctx, "m1", eventEnvelope("step_done", stub.requests[0].ExecutionID, 9, done)); err != nil {
		t.Fatal(err)
	}
	steps, _ := mysqlstore.GetPlanSteps(ctx, f.db, f.planID)
	if steps[0].Status != domain.StepExecuting {
		t.Fatalf("迟到结果不应推进状态，step1 = %q", steps[0].Status)
	}
}

func TestLoopSweepDispatches(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	stub := &stubRuntime{}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: stub, Owner: "r"}
	loop := &orchestration.Loop{DB: f.db, Runner: runner}

	loop.Sweep(context.Background())
	if stub.count() != 1 {
		t.Fatalf("Sweep 应派发第一个步骤，实际 %d", stub.count())
	}
}

//go:build integration

package orchestration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

func TestCancel(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	runtimeStub := &stubRuntime{cancelAck: true}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: runtimeStub, Owner: "runner-cancel"}
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	canceller := &orchestration.Canceller{DB: f.db, Runtime: runtimeStub}
	task, allAcked, err := canceller.RequestCancel(ctx, f.taskID)
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if task.Status != domain.TaskCancelling || !allAcked {
		t.Fatalf("task = %+v, allAcked=%v", task, allAcked)
	}

	// 取消后不再派发新步骤
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}
	if runtimeStub.count() != 1 {
		t.Fatalf("取消后仍派发: %d", runtimeStub.count())
	}

	if err := canceller.FinalizeCancel(ctx, f.taskID, false); err != nil {
		t.Fatalf("FinalizeCancel: %v", err)
	}
	task, _ = mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskCancelled {
		t.Fatalf("task = %q", task.Status)
	}
	step, _ := mysqlstore.GetStepByID(ctx, f.db, f.step1)
	if step.Status != domain.StepCancelled {
		t.Fatalf("step = %q", step.Status)
	}

	// 迟到的成功回调不得复活任务
	attempt, _ := mysqlstore.GetAttemptByExecution(ctx, f.db, runtimeStub.requests[0].ExecutionID)
	payload, _ := json.Marshal(map[string]any{"attempt_no": attempt.AttemptNo, "commit_sha": "abc"})
	eventEnv := protocol.Envelope{
		Type: protocol.TypeEvent, ExecutionID: attempt.ExecutionID,
		Payload: mustEventPayload(t, "step_done", payload),
	}
	handler := &orchestration.EventHandler{DB: f.db}
	if err := handler.Handle(ctx, "", eventEnv); err != nil {
		t.Fatal(err)
	}
	task, _ = mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskCancelled {
		t.Fatalf("迟到结果复活了任务: %q", task.Status)
	}
	step, _ = mysqlstore.GetStepByID(ctx, f.db, f.step1)
	if step.Status != domain.StepCancelled {
		t.Fatalf("迟到结果改变了步骤: %q", step.Status)
	}
}

func TestCancelWithoutAckStaysCancelling(t *testing.T) {
	f := setupRunner(t, 1, agent.Shared)
	ctx := context.Background()
	runtimeStub := &stubRuntime{cancelAck: false}
	runner := &orchestration.PlanRunner{DB: f.db, Runtime: runtimeStub, Owner: "runner-cancel"}
	if err := runner.RunOnce(ctx, f.taskID); err != nil {
		t.Fatal(err)
	}

	canceller := &orchestration.Canceller{DB: f.db, Runtime: runtimeStub}
	task, allAcked, err := canceller.RequestCancel(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if allAcked {
		t.Fatal("机器未确认时不应标记全部确认")
	}
	if task.Status != domain.TaskCancelling {
		t.Fatalf("task = %q", task.Status)
	}
	// 超时路径落终态
	if err := canceller.FinalizeCancel(ctx, f.taskID, true); err != nil {
		t.Fatal(err)
	}
	task, _ = mysqlstore.GetTask(ctx, f.db, f.taskID)
	if task.Status != domain.TaskCancelled {
		t.Fatalf("task = %q", task.Status)
	}
}

func mustEventPayload(t *testing.T, kind string, data []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(protocol.EventPayload{Kind: kind, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/protocol"
)

// EventHandler 处理 daemon 上行的 step_done / step_fail（D9/D10）。
type EventHandler struct {
	DB     *sql.DB
	Runner *PlanRunner
	Log    *slog.Logger
}

type stepDoneData struct {
	AttemptNo     uint32 `json:"attempt_no"`
	SummaryDigest string `json:"summary_digest"`
	CommitSHA     string `json:"commit_sha"`
	Branch        string `json:"branch"`
}

type stepFailData struct {
	AttemptNo  uint32 `json:"attempt_no"`
	Reason     string `json:"reason"`
	ErrorClass string `json:"error_class"`
	Uncertain  bool   `json:"uncertain"`
}

// Handle 实现 ws.Hub 的事件回调。
func (h *EventHandler) Handle(ctx context.Context, _ string, env protocol.Envelope) error {
	var payload protocol.EventPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil
	}
	switch payload.Kind {
	case "step_done":
		return h.complete(ctx, env, payload.Data)
	case "step_fail":
		return h.fail(ctx, env, payload.Data)
	default:
		return nil
	}
}

func (h *EventHandler) complete(ctx context.Context, env protocol.Envelope, data json.RawMessage) error {
	var done stepDoneData
	if err := json.Unmarshal(data, &done); err != nil {
		return nil
	}

	attempt, err := mysqlstore.GetAttemptByExecution(ctx, h.DB, env.ExecutionID)
	if errors.Is(err, sql.ErrNoRows) {
		h.log().Warn("收到未知执行的 step_done", "execution", env.ExecutionID)
		return nil
	}
	if err != nil {
		return err
	}
	if done.AttemptNo != 0 && done.AttemptNo != attempt.AttemptNo {
		h.log().Warn("拒绝迟到/串号的 step_done",
			"execution", env.ExecutionID, "got", done.AttemptNo, "want", attempt.AttemptNo)
		return nil
	}

	step, err := mysqlstore.GetStepByID(ctx, h.DB, attempt.StepID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if step.Status != domain.StepExecuting {
		h.log().Warn("步骤已非 executing，忽略 step_done", "step", step.ID, "status", step.Status)
		return nil
	}
	// 围栏：只有该步骤的最新 attempt 能推进状态（旧执行迟到的回调只记日志）
	latest, err := mysqlstore.GetLatestAttempt(ctx, h.DB, step.ID)
	if err != nil || latest.ID != attempt.ID {
		h.log().Warn("拒绝非当前 attempt 的 step_done",
			"execution", env.ExecutionID, "attempt", attempt.AttemptNo)
		return nil
	}

	if err := mysqlstore.UpdateAttemptStatus(ctx, h.DB, attempt.ID,
		domain.AttemptCompleted, "", done.SummaryDigest, done.CommitSHA); err != nil {
		return err
	}
	if _, err := mysqlstore.UpdateStepState(ctx, h.DB, step.ID,
		domain.StepExecuting, domain.StepCompleted, step.Version); err != nil {
		return err // 并发推进时由版本冲突拒绝
	}
	if plan, _, err := mysqlstore.GetPlanByID(ctx, h.DB, step.PlanID); err == nil {
		_, _ = mysqlstore.AppendActivity(ctx, h.DB, plan.TaskID, "step.done", map[string]any{
			"step_id": step.ID, "attempt_no": attempt.AttemptNo, "commit_sha": done.CommitSHA,
		})
	}
	return h.advance(ctx, step.PlanID)
}

func (h *EventHandler) fail(ctx context.Context, env protocol.Envelope, data json.RawMessage) error {
	var failed stepFailData
	if err := json.Unmarshal(data, &failed); err != nil {
		return nil
	}

	attempt, err := mysqlstore.GetAttemptByExecution(ctx, h.DB, env.ExecutionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if failed.AttemptNo != 0 && failed.AttemptNo != attempt.AttemptNo {
		h.log().Warn("拒绝迟到/串号的 step_fail",
			"execution", env.ExecutionID, "got", failed.AttemptNo, "want", attempt.AttemptNo)
		return nil
	}

	step, err := mysqlstore.GetStepByID(ctx, h.DB, attempt.StepID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if step.Status != domain.StepExecuting {
		return nil
	}

	stepTo := domain.StepFailed
	attemptStatus := domain.AttemptFailed
	if failed.Uncertain {
		stepTo = domain.StepReconciling
		attemptStatus = domain.AttemptUnknown
	}
	errorClass := domain.ErrorClass(failed.ErrorClass)
	if errorClass.Valid() != nil {
		errorClass = domain.ErrRuntime
	}

	if err := mysqlstore.UpdateAttemptStatus(ctx, h.DB, attempt.ID,
		attemptStatus, errorClass, "", ""); err != nil {
		return err
	}
	if _, err := mysqlstore.UpdateStepState(ctx, h.DB, step.ID,
		domain.StepExecuting, stepTo, step.Version); err != nil {
		return err
	}
	if plan, _, err := mysqlstore.GetPlanByID(ctx, h.DB, step.PlanID); err == nil {
		_, _ = mysqlstore.AppendActivity(ctx, h.DB, plan.TaskID, "step.fail", map[string]any{
			"step_id": step.ID, "attempt_no": attempt.AttemptNo,
			"error_class": string(errorClass), "uncertain": failed.Uncertain,
		})
	}
	return h.pause(ctx, step.PlanID)
}

// AdvancePlan 供对账等路径复用计划推进（E6）。
func (h *EventHandler) AdvancePlan(ctx context.Context, planID string) error {
	return h.advance(ctx, planID)
}

// PausePlan 供对账等路径复用计划暂停（E6）。
func (h *EventHandler) PausePlan(ctx context.Context, planID string) error {
	return h.pause(ctx, planID)
}

// advance 在步骤完成后决定下一步：还有其他步骤就继续派发，否则完成计划。
func (h *EventHandler) advance(ctx context.Context, planID string) error {
	plan, steps, err := mysqlstore.GetPlanByID(ctx, h.DB, planID)
	if err != nil {
		return err
	}

	allTerminal := true
	anyFailed := false
	for _, step := range steps {
		if !step.Status.IsTerminal() {
			allTerminal = false
		}
		if step.Status == domain.StepFailed || step.Status == domain.StepCancelled {
			anyFailed = true
		}
	}
	if !allTerminal {
		if h.Runner != nil {
			return h.Runner.RunOnce(ctx, plan.TaskID)
		}
		return nil
	}
	if anyFailed {
		return h.pause(ctx, planID)
	}

	if _, err := mysqlstore.UpdatePlanState(ctx, h.DB, plan.ID,
		domain.PlanExecuting, domain.PlanCompleted, plan.Version); err != nil {
		return nil
	}
	task, err := mysqlstore.GetTask(ctx, h.DB, plan.TaskID)
	if err != nil {
		return err
	}
	_, _ = mysqlstore.UpdateTaskState(ctx, h.DB, plan.TaskID,
		domain.TaskExecuting, domain.TaskChatting, task.Version)
	return nil
}

// pause 把计划与任务置为 paused（冲突时忽略：说明已被其他路径推进）。
func (h *EventHandler) pause(ctx context.Context, planID string) error {
	plan, _, err := mysqlstore.GetPlanByID(ctx, h.DB, planID)
	if err != nil {
		return err
	}
	if _, err := mysqlstore.UpdatePlanState(ctx, h.DB, plan.ID,
		domain.PlanExecuting, domain.PlanPaused, plan.Version); err != nil {
		return nil
	}
	task, err := mysqlstore.GetTask(ctx, h.DB, plan.TaskID)
	if err != nil {
		return err
	}
	_, _ = mysqlstore.UpdateTaskState(ctx, h.DB, plan.TaskID,
		domain.TaskExecuting, domain.TaskPaused, task.Version)
	return nil
}

func (h *EventHandler) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

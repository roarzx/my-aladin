package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Canceller 实现取消流程（E9）：先撤销调度资格，再向机器发停止请求。
type Canceller struct {
	DB      *sql.DB
	Runtime runtime.Runtime
	Log     *slog.Logger
}

// RequestCancel 把任务置为 cancelling，并向执行中的步骤发送 stop_session。
//
// 机器确认停止前保持 cancelling；未确认的执行记入 orphan 清单（activities 审计）。
func (c *Canceller) RequestCancel(ctx context.Context, taskID string) (domain.Task, bool, error) {
	task, err := mysqlstore.GetTask(ctx, c.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, false, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.Task{}, false, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	if task.Status == domain.TaskCancelling || task.Status == domain.TaskCancelled {
		return task, false, nil // 幂等；仍有执行未确认
	}
	if err := task.CanTransition(domain.EvTaskCancelRequested); err != nil {
		return domain.Task{}, false, errcode.Newf(errcode.AlreadyTerminal,
			"当前状态 %s 不能取消", task.Status)
	}
	version, err := mysqlstore.SetCancelRequested(ctx, c.DB, taskID, task.Status, task.Version)
	if err != nil {
		return domain.Task{}, false, err
	}
	task.Status = domain.TaskCancelling
	task.Version = version
	now := time.Now().UTC()
	task.CancelRequestedAt = &now

	steps, err := c.activeSteps(ctx, taskID)
	if err != nil {
		return domain.Task{}, false, err
	}
	allAcknowledged := true
	for _, step := range steps {
		attempt, err := mysqlstore.GetLatestAttempt(ctx, c.DB, step.ID)
		if err != nil {
			continue
		}
		acknowledged := false
		if c.Runtime != nil {
			ack, err := c.Runtime.Cancel(ctx, attempt.ExecutionID)
			if err != nil {
				c.log().Warn("停止请求失败", "execution", attempt.ExecutionID, "err", err)
			}
			acknowledged = ack
		}
		_, _ = mysqlstore.AppendActivity(ctx, c.DB, taskID, "task.cancel_requested", map[string]any{
			"step_id": step.ID, "execution_id": attempt.ExecutionID,
			"stop_acknowledged": acknowledged,
		})
		if !acknowledged {
			allAcknowledged = false
			// 机器未确认：记为 orphan，迟到的结果只写审计
			_, _ = mysqlstore.AppendActivity(ctx, c.DB, taskID, "execution.orphaned", map[string]any{
				"step_id": step.ID, "execution_id": attempt.ExecutionID,
			})
		}
	}
	return task, allAcknowledged, nil
}

// FinalizeCancel 在机器确认停止或取消超时后落终态：Plan stopped、Step cancelled。
//
// 迟到的成功结果不会复活任务：EventHandler 只处理 executing 步骤。
func (c *Canceller) FinalizeCancel(ctx context.Context, taskID string, timeout bool) error {
	task, err := mysqlstore.GetTask(ctx, c.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	if task.Status == domain.TaskCancelled {
		return nil
	}
	if task.Status != domain.TaskCancelling {
		return errcode.Newf(errcode.AlreadyTerminal, "任务不在 cancelling（当前 %s）", task.Status)
	}

	revision, err := mysqlstore.LatestPlanRevision(ctx, c.DB, taskID)
	if err == nil && revision > 0 {
		if plan, steps, err := mysqlstore.GetPlanByRevision(ctx, c.DB, taskID, revision); err == nil {
			for _, step := range steps {
				from := step.Status
				if from != domain.StepExecuting && from != domain.StepReconciling {
					continue
				}
				if _, err := mysqlstore.UpdateStepState(ctx, c.DB, step.ID,
					from, domain.StepCancelled, step.Version); err != nil {
					c.log().Warn("取消步骤失败", "step", step.ID, "err", err)
				}
			}
			if plan.Status == domain.PlanExecuting || plan.Status == domain.PlanPaused {
				_, _ = mysqlstore.UpdatePlanState(ctx, c.DB, plan.ID,
					plan.Status, domain.PlanStopped, plan.Version)
			}
		}
	}

	event := domain.EvTaskCancelConfirmed
	if timeout {
		event = domain.EvTaskCancelTimeout
	}
	if err := task.CanTransition(event); err != nil {
		return errcode.Newf(errcode.Conflict, "任务状态不允许取消确认: %v", err)
	}
	if _, err := mysqlstore.UpdateTaskState(ctx, c.DB, taskID,
		domain.TaskCancelling, domain.TaskCancelled, task.Version); err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, c.DB, taskID, "task.cancelled", map[string]any{
		"timeout": timeout,
	})
	return nil
}

func (c *Canceller) activeSteps(ctx context.Context, taskID string) ([]domain.Step, error) {
	revision, err := mysqlstore.LatestPlanRevision(ctx, c.DB, taskID)
	if err != nil || revision == 0 {
		return nil, err
	}
	_, steps, err := mysqlstore.GetPlanByRevision(ctx, c.DB, taskID, revision)
	if err != nil {
		return nil, err
	}
	var active []domain.Step
	for _, step := range steps {
		if step.Status == domain.StepExecuting || step.Status == domain.StepReconciling {
			active = append(active, step)
		}
	}
	return active, nil
}

func (c *Canceller) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

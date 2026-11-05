package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// PlanRunner 串行推进普通模式的步骤（D8）。
type PlanRunner struct {
	DB      *sql.DB
	Runtime runtime.Runtime
	Owner   string
	Lock    *TaskLock
	Log     *slog.Logger
}

// RunOnce 最多派发一个步骤；已有 executing 步骤时不做任何事。
func (r *PlanRunner) RunOnce(ctx context.Context, taskID string) error {
	if r.Lock != nil {
		release, err := r.Lock.Acquire(ctx, taskID, r.Owner)
		if err != nil {
			if errors.Is(err, ErrTaskLocked) {
				return nil
			}
			return err
		}
		defer release()
	}

	task, err := mysqlstore.GetTask(ctx, r.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Status != domain.TaskExecuting {
		return nil
	}

	latest, err := mysqlstore.LatestPlanRevision(ctx, r.DB, taskID)
	if err != nil || latest == 0 {
		return err
	}
	_, steps, err := mysqlstore.GetPlanByRevision(ctx, r.DB, taskID, latest)
	if err != nil {
		return err
	}

	next := nextStep(steps)
	if next == nil {
		return nil
	}

	agents, err := mysqlstore.GetTaskAgents(ctx, r.DB, taskID)
	if err != nil {
		return err
	}
	agentID, ok := agents[next.AgentRef]
	if !ok {
		return r.failStep(ctx, *next, errcode.Newf(errcode.AgentRefInvalid,
			"agent_ref 不在任务白名单: %s", next.AgentRef))
	}
	a, err := mysqlstore.GetAgent(ctx, r.DB, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return r.failStep(ctx, *next, errcode.New(errcode.AgentUnavailable, "Agent 不存在"))
	}
	if err != nil {
		return err
	}
	if err := a.SchedulableBy(task.CreatedBy); err != nil {
		return r.failStep(ctx, *next, err)
	}

	newVersion, err := mysqlstore.UpdateStepState(ctx, r.DB, next.ID,
		domain.StepPending, domain.StepExecuting, next.Version)
	if err != nil {
		return err
	}
	next.Version = newVersion

	attemptNo, err := mysqlstore.NextAttemptNo(ctx, r.DB, next.ID)
	if err != nil {
		return err
	}
	executionID := idgen.New()
	attempt := domain.Attempt{
		ID: idgen.New(), StepID: next.ID, AttemptNo: attemptNo,
		ExecutionID: executionID, LeaseEpoch: 1, Status: domain.AttemptDispatched,
	}
	if err := mysqlstore.InsertAttempt(ctx, r.DB, attempt); err != nil {
		_, _ = mysqlstore.UpdateStepState(ctx, r.DB, next.ID,
			domain.StepExecuting, domain.StepFailed, next.Version)
		return err
	}

	if r.Runtime != nil {
		_, dispatchErr := r.Runtime.Dispatch(ctx, runtime.DispatchRequest{
			ExecutionID: executionID,
			TaskID:      taskID,
			StepID:      next.ID,
			AttemptNo:   attemptNo,
			Branch:      task.PlanBranch,
		})
		if dispatchErr != nil {
			_, _ = mysqlstore.UpdateStepState(ctx, r.DB, next.ID,
				domain.StepExecuting, domain.StepFailed, next.Version)
			return dispatchErr
		}
	}

	if r.Log != nil {
		r.Log.Info("步骤已派发", "task", taskID, "step", next.ID, "attempt", attemptNo, "execution", executionID)
	}
	return nil
}

// nextStep 返回下一个可派发步骤：存在 executing 时返回 nil（串行约束）。
func nextStep(steps []domain.Step) *domain.Step {
	for i := range steps {
		if steps[i].Status == domain.StepExecuting {
			return nil
		}
	}
	for i := range steps {
		if steps[i].Status == domain.StepPending {
			return &steps[i]
		}
	}
	return nil
}

func (r *PlanRunner) failStep(ctx context.Context, step domain.Step, cause error) error {
	from := step.Status
	if from != domain.StepPending && from != domain.StepExecuting {
		return cause
	}
	_, _ = mysqlstore.UpdateStepState(ctx, r.DB, step.ID, from, domain.StepFailed, step.Version)
	return cause
}

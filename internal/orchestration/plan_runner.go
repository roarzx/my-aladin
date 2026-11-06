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
	Lease   *Lease
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

	// 任务可能绑定多个仓库，当前串行执行仅使用第一个（按 URL 排序）。
	repos, err := mysqlstore.ListTaskRepositories(ctx, r.DB, taskID)
	if err != nil {
		return err
	}
	var repo mysqlstore.TaskRepository
	if len(repos) > 0 {
		repo = repos[0]
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
	if r.Lease != nil {
		if err := r.Lease.Acquire(ctx, attempt); err != nil {
			_, _ = mysqlstore.UpdateStepState(ctx, r.DB, next.ID,
				domain.StepExecuting, domain.StepFailed, next.Version)
			_ = mysqlstore.UpdateAttemptStatus(ctx, r.DB, attempt.ID,
				domain.AttemptFailed, domain.ErrRuntime, "", "")
			return err
		}
	}

	if r.Runtime != nil {
		_, dispatchErr := r.Runtime.Dispatch(ctx, runtime.DispatchRequest{
			ExecutionID:       executionID,
			MachineID:         a.MachineID,
			TaskID:            taskID,
			StepID:            next.ID,
			AttemptNo:         attemptNo,
			LeaseEpoch:        attempt.LeaseEpoch,
			Branch:            task.PlanBranch,
			Prompt:            stepPrompt(task.Name, *next),
			WorkDir:           a.WorkDir,
			RepoURL:           repo.URL,
			RepoDefaultBranch: repo.DefaultBranch,
			SystemPrompt:      a.SystemPrompt,
			TaskTitle:         task.Name,
			StepTitle:         next.Title,
		})
		if dispatchErr != nil {
			_, _ = mysqlstore.UpdateStepState(ctx, r.DB, next.ID,
				domain.StepExecuting, domain.StepFailed, next.Version)
			return dispatchErr
		}
	}

	_, _ = mysqlstore.AppendActivity(ctx, r.DB, taskID, "step.dispatched", map[string]any{
		"step_id": next.ID, "attempt_no": attemptNo, "execution_id": executionID,
	})

	if r.Log != nil {
		r.Log.Info("步骤已派发", "task", taskID, "step", next.ID, "attempt", attemptNo, "execution", executionID)
	}
	return nil
}

// stepPrompt 按 D9 的格式拼接步骤提示词：任务名 + 步骤标题 + 步骤正文。
func stepPrompt(taskName string, step domain.Step) string {
	return taskName + "\n\n" + step.Title + "\n" + step.Body
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

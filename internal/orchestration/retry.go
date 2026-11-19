package orchestration

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// RetryService 实现失败步骤的人工重试（D11）。
type RetryService struct {
	DB          *sql.DB
	Runtime     runtime.Runtime
	MaxAttempts int
	// Lease 为重试尝试申请执行租约，避免被 Watchdog 误判为过期（E4）。
	Lease *Lease
}

// Retry 为 failed 步骤创建新 attempt 并重新派发。
func (s *RetryService) Retry(ctx context.Context, stepID, instruction string) (uint32, error) {
	step, err := mysqlstore.GetStepByID(ctx, s.DB, stepID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errcode.New(errcode.NotFound, "步骤不存在")
	}
	if err != nil {
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取步骤失败")
	}
	if step.Status != domain.StepFailed {
		return 0, errcode.Newf(errcode.StepNotFailed, "步骤状态为 %s", step.Status)
	}

	attemptNo, err := mysqlstore.NextAttemptNo(ctx, s.DB, stepID)
	if err != nil {
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取尝试号失败")
	}
	if s.MaxAttempts > 0 && int(attemptNo) > s.MaxAttempts {
		return 0, errcode.Newf(errcode.RetryBudgetExceeded, "已达上限 %d 次", s.MaxAttempts)
	}

	plan, _, err := mysqlstore.GetPlanByID(ctx, s.DB, step.PlanID)
	if err != nil {
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取计划失败")
	}
	task, err := mysqlstore.GetTask(ctx, s.DB, plan.TaskID)
	if err != nil {
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	agents, err := mysqlstore.GetTaskAgents(ctx, s.DB, plan.TaskID)
	if err != nil {
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取任务成员失败")
	}
	agentID, ok := agents[step.AgentRef]
	if !ok {
		return 0, errcode.Newf(errcode.AgentRefInvalid, "agent_ref 不在白名单: %s", step.AgentRef)
	}
	agentConfig, err := mysqlstore.GetAgent(ctx, s.DB, agentID)
	if err != nil {
		return 0, errcode.New(errcode.AgentUnavailable, "Agent 配置不存在")
	}
	if err := agentConfig.SchedulableBy(task.CreatedBy); err != nil {
		return 0, err
	}

	newVersion, err := mysqlstore.UpdateStepState(ctx, s.DB, stepID,
		domain.StepFailed, domain.StepExecuting, step.Version)
	if err != nil {
		return 0, err
	}
	step.Version = newVersion

	executionID := idgen.New()
	attempt := domain.Attempt{
		ID: idgen.New(), StepID: stepID, AttemptNo: attemptNo,
		ExecutionID: executionID, LeaseEpoch: 1, Status: domain.AttemptDispatched,
	}
	if err := mysqlstore.InsertAttempt(ctx, s.DB, attempt); err != nil {
		_, _ = mysqlstore.UpdateStepState(ctx, s.DB, stepID,
			domain.StepExecuting, domain.StepFailed, step.Version)
		return 0, errcode.New(errcode.UpstreamUnavailable, "创建尝试失败")
	}

	// 与 plan_runner 一致：先建租约再派发，否则 Watchdog 的租约扫描会立刻
	// 把这次重试判为 lease_expired 并暂停任务。
	if s.Lease != nil {
		if err := s.Lease.Acquire(ctx, attempt); err != nil {
			_ = mysqlstore.UpdateAttemptStatus(ctx, s.DB, attempt.ID,
				domain.AttemptFailed, domain.ErrRuntime, "", "")
			_, _ = mysqlstore.UpdateStepState(ctx, s.DB, stepID,
				domain.StepExecuting, domain.StepFailed, step.Version)
			return 0, errcode.Newf(errcode.RuntimeStartFailed, "申请执行租约失败: %v", err)
		}
	}

	// 计划/任务从 paused 恢复
	if plan.Status == domain.PlanPaused {
		if _, err := mysqlstore.UpdatePlanState(ctx, s.DB, plan.ID,
			domain.PlanPaused, domain.PlanExecuting, plan.Version); err != nil {
			_ = err // 由 Watchdog 兜底
		}
	}
	if task.Status == domain.TaskPaused {
		if _, err := mysqlstore.UpdateTaskState(ctx, s.DB, task.ID,
			domain.TaskPaused, domain.TaskExecuting, task.Version); err != nil {
			_ = err
		}
	}

	// 任务可能绑定多个仓库，与 plan_runner 一致只使用第一个。
	repos, err := mysqlstore.ListTaskRepositories(ctx, s.DB, task.ID)
	if err != nil {
		s.failDispatch(ctx, attempt, step, step.Version, "读取任务仓库失败")
		return 0, errcode.New(errcode.UpstreamUnavailable, "读取任务仓库失败")
	}
	var repo mysqlstore.TaskRepository
	if len(repos) > 0 {
		repo = repos[0]
	}

	if s.Runtime != nil {
		if _, err := s.Runtime.Dispatch(ctx, runtime.DispatchRequest{
			ExecutionID:       executionID,
			MachineID:         agentConfig.MachineID,
			TaskID:            task.ID,
			StepID:            stepID,
			AttemptNo:         attemptNo,
			LeaseEpoch:        attempt.LeaseEpoch,
			Branch:            task.PlanBranch,
			Prompt:            stepPrompt(task.Name, step),
			WorkDir:           agentConfig.WorkDir,
			RepoURL:           repo.URL,
			RepoDefaultBranch: repo.DefaultBranch,
			SystemPrompt:      agentConfig.SystemPrompt,
			TaskTitle:         task.Name,
			StepTitle:         step.Title,
		}); err != nil {
			s.failDispatch(ctx, attempt, step, step.Version, "重试派发失败")
			return 0, errcode.Newf(errcode.RuntimeStartFailed, "重试派发失败: %v", err)
		}
	}
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, task.ID, "step.dispatched", map[string]any{
		"step_id": stepID, "attempt_no": attemptNo, "execution_id": executionID,
	})
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, task.ID, "step.retried", map[string]any{
		"step_id": stepID, "attempt_no": attemptNo,
	})
	_ = instruction // 重试指令随 D12 的上下文注入使用
	return attemptNo, nil
}

// failDispatch 回滚一次派发失败：释放租约、尝试置失败、步骤置失败。
func (s *RetryService) failDispatch(
	ctx context.Context, attempt domain.Attempt, step domain.Step, version uint64, reason string,
) {
	if s.Lease != nil {
		_ = s.Lease.Release(ctx, attempt.ID)
	}
	_ = mysqlstore.UpdateAttemptStatus(ctx, s.DB, attempt.ID,
		domain.AttemptFailed, domain.ErrRuntime, "", "")
	_, _ = mysqlstore.UpdateStepState(ctx, s.DB, step.ID,
		domain.StepExecuting, domain.StepFailed, version)
	if plan, _, err := mysqlstore.GetPlanByID(ctx, s.DB, step.PlanID); err == nil {
		_, _ = mysqlstore.AppendActivity(ctx, s.DB, plan.TaskID, "step.fail", map[string]any{
			"step_id": step.ID, "attempt_no": attempt.AttemptNo,
			"error_class": string(domain.ErrRuntime), "uncertain": false, "reason": reason,
		})
	}
}

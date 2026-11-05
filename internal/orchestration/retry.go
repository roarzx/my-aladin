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

	if s.Runtime != nil {
		if _, err := s.Runtime.Dispatch(ctx, runtime.DispatchRequest{
			ExecutionID: executionID,
			MachineID:   agentConfig.MachineID,
			TaskID:      task.ID,
			StepID:      stepID,
			AttemptNo:   attemptNo,
			Branch:      task.PlanBranch,
		}); err != nil {
			_, _ = mysqlstore.UpdateStepState(ctx, s.DB, stepID,
				domain.StepExecuting, domain.StepFailed, step.Version)
			_ = mysqlstore.UpdateAttemptStatus(ctx, s.DB, attempt.ID,
				domain.AttemptFailed, domain.ErrRuntime, "", "")
			return 0, errcode.Newf(errcode.RuntimeStartFailed, "重试派发失败: %v", err)
		}
	}
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, task.ID, "step.retried", map[string]any{
		"step_id": stepID, "attempt_no": attemptNo,
	})
	_ = instruction // 重试指令随 D12 的上下文注入使用
	return attemptNo, nil
}

package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// LatestPlanRevision 返回任务的最新计划版本；不存在时返回 0。
func LatestPlanRevision(ctx context.Context, ex Executor, taskID string) (int, error) {
	var revision sql.NullInt64
	if err := ex.QueryRowContext(ctx,
		`SELECT MAX(revision) FROM plans WHERE task_id = ?`, taskID).Scan(&revision); err != nil {
		return 0, fmt.Errorf("查询计划版本: %w", err)
	}
	if !revision.Valid {
		return 0, nil
	}
	return int(revision.Int64), nil
}

// InsertPlan 写入一个计划版本。
func InsertPlan(ctx context.Context, ex Executor, plan domain.Plan) error {
	if err := plan.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO plans (id, task_id, revision, content_hash, content_md, status, version)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		plan.ID, plan.TaskID, plan.Revision, plan.ContentHash, plan.ContentMD,
		string(plan.Status), plan.Version)
	if err != nil {
		return fmt.Errorf("写入计划: %w", err)
	}
	return nil
}

// InsertPlanStep 写入一个步骤。
func InsertPlanStep(ctx context.Context, ex Executor, step domain.Step) error {
	if err := step.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO plan_steps (id, plan_id, ordinal, title, body, agent_ref, status, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.PlanID, step.Ordinal, step.Title, step.Body, step.AgentRef,
		string(step.Status), step.Version)
	if err != nil {
		return fmt.Errorf("写入计划步骤: %w", err)
	}
	return nil
}

// GetPlanByID 读取计划及步骤。
func GetPlanByID(ctx context.Context, ex Executor, planID string) (domain.Plan, []domain.Step, error) {
	var (
		plan   domain.Plan
		status string
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, task_id, revision, content_hash, content_md, status, version
		FROM plans WHERE id = ?`, planID).Scan(
		&plan.ID, &plan.TaskID, &plan.Revision, &plan.ContentHash, &plan.ContentMD,
		&status, &plan.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, nil, sql.ErrNoRows
	}
	if err != nil {
		return domain.Plan{}, nil, fmt.Errorf("读取计划: %w", err)
	}
	plan.Status = domain.PlanStatus(status)

	steps, err := GetPlanSteps(ctx, ex, planID)
	if err != nil {
		return domain.Plan{}, nil, err
	}
	return plan, steps, nil
}

// GetPlanByRevision 按任务与版本读取计划。
func GetPlanByRevision(ctx context.Context, ex Executor, taskID string, revision int) (domain.Plan, []domain.Step, error) {
	var planID string
	err := ex.QueryRowContext(ctx,
		`SELECT id FROM plans WHERE task_id = ? AND revision = ?`, taskID, revision).Scan(&planID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, nil, sql.ErrNoRows
	}
	if err != nil {
		return domain.Plan{}, nil, fmt.Errorf("读取计划版本: %w", err)
	}
	return GetPlanByID(ctx, ex, planID)
}

// GetPlanSteps 读取计划步骤（按 ordinal）。
func GetPlanSteps(ctx context.Context, ex Executor, planID string) ([]domain.Step, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT id, plan_id, ordinal, title, body, agent_ref, status, version
		FROM plan_steps WHERE plan_id = ? ORDER BY ordinal`, planID)
	if err != nil {
		return nil, fmt.Errorf("读取计划步骤: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []domain.Step
	for rows.Next() {
		var (
			step   domain.Step
			status string
		)
		if err := rows.Scan(&step.ID, &step.PlanID, &step.Ordinal, &step.Title, &step.Body,
			&step.AgentRef, &status, &step.Version); err != nil {
			return nil, err
		}
		step.Status = domain.StepStatus(status)
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

// InsertConfirmation 写入确认；同 (plan, revision) 重复确认幂等。
func InsertConfirmation(ctx context.Context, ex Executor, planID string, revision int,
	contentHash string, actorID uint64) (domain.Confirmation, error) {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO confirmations (id, plan_id, revision, content_hash, actor_id)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = id`,
		newID(), planID, revision, contentHash, actorID)
	if err != nil {
		return domain.Confirmation{}, fmt.Errorf("写入确认: %w", err)
	}
	return GetConfirmation(ctx, ex, planID, revision)
}

// GetConfirmation 读取确认。
func GetConfirmation(ctx context.Context, ex Executor, planID string, revision int) (domain.Confirmation, error) {
	var confirmation domain.Confirmation
	err := ex.QueryRowContext(ctx, `
		SELECT id, plan_id, revision, content_hash, actor_id, created_at
		FROM confirmations WHERE plan_id = ? AND revision = ?`, planID, revision).Scan(
		&confirmation.ID, &confirmation.PlanID, &confirmation.Revision,
		&confirmation.ContentHash, &confirmation.ActorID, &confirmation.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Confirmation{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Confirmation{}, fmt.Errorf("读取确认: %w", err)
	}
	return confirmation, nil
}

// UpdateTaskState 以 (status, version) 为条件更新任务状态（乐观并发）。
func UpdateTaskState(ctx context.Context, ex Executor, taskID string,
	from, to domain.TaskStatus, expectedVersion uint64) (uint64, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE tasks SET status = ?, version = version + 1, updated_at = UTC_TIMESTAMP(3)
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), taskID, string(from), expectedVersion)
	if err != nil {
		return 0, fmt.Errorf("更新任务状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, errcode.Newf(errcode.Conflict, "任务状态或版本已变化")
	}
	return expectedVersion + 1, nil
}

// UpdatePlanState 以 (status, version) 为条件更新计划状态。
func UpdatePlanState(ctx context.Context, ex Executor, planID string,
	from, to domain.PlanStatus, expectedVersion uint64) (uint64, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE plans SET status = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), planID, string(from), expectedVersion)
	if err != nil {
		return 0, fmt.Errorf("更新计划状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, errcode.Newf(errcode.Conflict, "计划状态或版本已变化")
	}
	return expectedVersion + 1, nil
}

// UpdateStepState 以 (status, version) 为条件更新步骤状态。
func UpdateStepState(ctx context.Context, ex Executor, stepID string,
	from, to domain.StepStatus, expectedVersion uint64) (uint64, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE plan_steps SET status = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), stepID, string(from), expectedVersion)
	if err != nil {
		return 0, fmt.Errorf("更新步骤状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, errcode.Newf(errcode.Conflict, "步骤状态或版本已变化")
	}
	return expectedVersion + 1, nil
}

// GetTaskAgents 返回任务内活跃的 agent_ref → agent_id。
func GetTaskAgents(ctx context.Context, ex Executor, taskID string) (map[string]string, error) {
	rows, err := ex.QueryContext(ctx,
		`SELECT agent_ref, agent_id FROM task_agents WHERE task_id = ? AND active = 1`, taskID)
	if err != nil {
		return nil, fmt.Errorf("读取任务成员: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]string)
	for rows.Next() {
		var ref, agentID string
		if err := rows.Scan(&ref, &agentID); err != nil {
			return nil, err
		}
		out[ref] = agentID
	}
	return out, rows.Err()
}

// NextAttemptNo 返回步骤的下一个尝试号（从 1 开始）。
func NextAttemptNo(ctx context.Context, ex Executor, stepID string) (uint32, error) {
	var next sql.NullInt64
	if err := ex.QueryRowContext(ctx,
		`SELECT MAX(attempt_no) FROM attempts WHERE step_id = ?`, stepID).Scan(&next); err != nil {
		return 0, fmt.Errorf("查询尝试号: %w", err)
	}
	if !next.Valid {
		return 1, nil
	}
	return uint32(next.Int64) + 1, nil
}

// InsertAttempt 写入一次执行尝试。
func InsertAttempt(ctx context.Context, ex Executor, attempt domain.Attempt) error {
	if err := attempt.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO attempts (id, step_id, attempt_no, execution_id, lease_epoch, status,
			error_class, result_digest, commit_sha)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.ID, attempt.StepID, attempt.AttemptNo, attempt.ExecutionID,
		attempt.LeaseEpoch, string(attempt.Status),
		nullString(string(attempt.ErrorClass)), nullString(attempt.ResultDigest),
		nullString(attempt.CommitSHA))
	if err != nil {
		return fmt.Errorf("写入执行尝试: %w", err)
	}
	return nil
}

// GetAttemptByExecution 按 execution_id 读取尝试。
func GetAttemptByExecution(ctx context.Context, ex Executor, executionID string) (domain.Attempt, error) {
	var (
		attempt    domain.Attempt
		status     string
		errorClass sql.NullString
		digest     sql.NullString
		commit     sql.NullString
		createdAt  time.Time
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, step_id, attempt_no, execution_id, lease_epoch, status,
		       error_class, result_digest, commit_sha, created_at
		FROM attempts WHERE execution_id = ?`, executionID).Scan(
		&attempt.ID, &attempt.StepID, &attempt.AttemptNo, &attempt.ExecutionID,
		&attempt.LeaseEpoch, &status, &errorClass, &digest, &commit, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attempt{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Attempt{}, fmt.Errorf("读取执行尝试: %w", err)
	}
	attempt.Status = domain.AttemptStatus(status)
	attempt.ErrorClass = domain.ErrorClass(errorClass.String)
	attempt.ResultDigest = digest.String
	attempt.CommitSHA = commit.String
	attempt.CreatedAt = createdAt
	return attempt, nil
}

func newID() string {
	return idgen.New()
}

// ListTaskIDsByStatus 返回指定状态的任务 ID（供派发循环使用）。
func ListTaskIDsByStatus(ctx context.Context, ex Executor, status domain.TaskStatus, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := ex.QueryContext(ctx,
		`SELECT id FROM tasks WHERE status = ? ORDER BY updated_at LIMIT ?`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("列出任务: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetStepByID 按 ID 读取步骤。
func GetStepByID(ctx context.Context, ex Executor, id string) (domain.Step, error) {
	var (
		step   domain.Step
		status string
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, plan_id, ordinal, title, body, agent_ref, status, version
		FROM plan_steps WHERE id = ?`, id).Scan(
		&step.ID, &step.PlanID, &step.Ordinal, &step.Title, &step.Body,
		&step.AgentRef, &status, &step.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Step{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Step{}, fmt.Errorf("读取步骤: %w", err)
	}
	step.Status = domain.StepStatus(status)
	return step, nil
}

// UpdateAttemptStatus 更新执行尝试的状态与证据。
func UpdateAttemptStatus(ctx context.Context, ex Executor, id string, status domain.AttemptStatus,
	errorClass domain.ErrorClass, resultDigest, commitSHA string) error {
	if err := status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		UPDATE attempts SET status = ?, error_class = ?, result_digest = ?, commit_sha = ?
		WHERE id = ?`,
		string(status), nullString(string(errorClass)), nullString(resultDigest), nullString(commitSHA), id)
	if err != nil {
		return fmt.Errorf("更新执行尝试: %w", err)
	}
	return nil
}

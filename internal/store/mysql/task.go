package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
)

// InsertTask 写入任务记录。
func InsertTask(ctx context.Context, ex Executor, t domain.Task) error {
	if err := t.Mode.Valid(); err != nil {
		return err
	}
	if err := t.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO tasks (id, workspace_id, created_by, name, mode, status, version,
			activity_seq, plan_branch, config_snapshot, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.WorkspaceID, t.CreatedBy, t.Name, string(t.Mode), string(t.Status),
		t.Version, t.ActivitySeq, nullString(t.PlanBranch), []byte(t.ConfigSnapshot),
		t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("写入任务: %w", err)
	}
	return nil
}

// InsertTaskAgent 写入任务成员。
func InsertTaskAgent(ctx context.Context, ex Executor, taskID, agentRef, agentID string, role domain.TaskAgentRole) error {
	if err := role.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx,
		`INSERT INTO task_agents (task_id, agent_ref, agent_id, role) VALUES (?, ?, ?, ?)`,
		taskID, agentRef, agentID, string(role))
	if err != nil {
		return fmt.Errorf("写入 task_agent: %w", err)
	}
	return nil
}

// InsertTaskRepository 绑定仓库；base_sha 与目标分支在启动时补齐（D6）。
func InsertTaskRepository(ctx context.Context, ex Executor, taskID, repositoryID, baseSHA, targetBranch string) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO task_repositories (task_id, repository_id, base_sha, target_branch) VALUES (?, ?, ?, ?)`,
		taskID, repositoryID, baseSHA, targetBranch)
	if err != nil {
		return fmt.Errorf("写入 task_repository: %w", err)
	}
	return nil
}

// GetTask 按 ID 读取任务；不存在返回 sql.ErrNoRows。
func GetTask(ctx context.Context, ex Executor, id string) (domain.Task, error) {
	var (
		t          domain.Task
		mode       string
		status     string
		planBranch sql.NullString
		cancelAt   sql.NullTime
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, workspace_id, created_by, name, mode, status, version, activity_seq,
		       plan_branch, config_snapshot, cancel_requested_at, created_at, updated_at
		FROM tasks WHERE id = ?`, id).Scan(
		&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Name, &mode, &status, &t.Version,
		&t.ActivitySeq, &planBranch, &t.ConfigSnapshot, &cancelAt, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("读取任务: %w", err)
	}
	t.Mode = domain.TaskMode(mode)
	t.Status = domain.TaskStatus(status)
	t.PlanBranch = planBranch.String
	if cancelAt.Valid {
		v := cancelAt.Time
		t.CancelRequestedAt = &v
	}
	return t, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

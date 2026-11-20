package mysql

import (
	"context"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

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

// ListTaskIDsByStatus 按状态列出任务 ID（Runner 扫描用）。
func ListTaskIDsByStatus(ctx context.Context, ex Executor, status domain.TaskStatus, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := ex.QueryContext(ctx,
		`SELECT id FROM tasks WHERE status = ? ORDER BY updated_at ASC LIMIT ?`,
		string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("列出任务: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetTaskAgents 返回任务内的 agent_ref → agent_id 映射。
func GetTaskAgents(ctx context.Context, ex Executor, taskID string) (map[string]string, error) {
	rows, err := ex.QueryContext(ctx,
		`SELECT agent_ref, agent_id FROM task_agents WHERE task_id = ?`, taskID)
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

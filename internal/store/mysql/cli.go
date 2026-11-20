package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
)

// TaskFilter 是 CLI `issue list` 的过滤条件；零值表示不过滤。
type TaskFilter struct {
	Keyword     string
	Status      domain.TaskStatus
	CreatedFrom time.Time
	CreatedTo   time.Time
	UpdatedFrom time.Time
	UpdatedTo   time.Time
	Limit       int
}

// ListTasksInWorkspace 按过滤条件列出工作区内的任务（CLI issue list）。
func ListTasksInWorkspace(ctx context.Context, ex Executor, workspaceID string, f TaskFilter) ([]domain.Task, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	where := []string{"workspace_id = ?"}
	args := []any{workspaceID}
	if keyword := strings.TrimSpace(f.Keyword); keyword != "" {
		where = append(where, "(name LIKE ? OR id LIKE ? OR requirement LIKE ?)")
		like := "%" + keyword + "%"
		args = append(args, like, like, like)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	if !f.CreatedFrom.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, f.CreatedFrom)
	}
	if !f.CreatedTo.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, f.CreatedTo)
	}
	if !f.UpdatedFrom.IsZero() {
		where = append(where, "updated_at >= ?")
		args = append(args, f.UpdatedFrom)
	}
	if !f.UpdatedTo.IsZero() {
		where = append(where, "updated_at <= ?")
		args = append(args, f.UpdatedTo)
	}
	args = append(args, limit)

	rows, err := ex.QueryContext(ctx, `
		SELECT id, workspace_id, created_by, name, requirement, inform_doc,
		       mode, status, version, activity_seq, plan_branch, config_snapshot,
		       cancel_requested_at, created_at, updated_at
		FROM tasks
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY updated_at DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("列出任务: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Task
	for rows.Next() {
		var (
			t           domain.Task
			mode        string
			status      string
			requirement sql.NullString
			informDoc   sql.NullString
			planBranch  sql.NullString
			cancelAt    sql.NullTime
		)
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.CreatedBy, &t.Name,
			&requirement, &informDoc, &mode, &status, &t.Version, &t.ActivitySeq,
			&planBranch, &t.ConfigSnapshot, &cancelAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Requirement = requirement.String
		t.InformDoc = informDoc.String
		t.Mode = domain.TaskMode(mode)
		t.Status = domain.TaskStatus(status)
		t.PlanBranch = planBranch.String
		if cancelAt.Valid {
			v := cancelAt.Time
			t.CancelRequestedAt = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TaskAgentOnMachine 判断任务是否分配了运行在该机器上的 Agent。//
// CLI 的写操作（上报状态、请求输入）只允许来自真正在跑这个 Issue 的机器，
// 避免任意机器令牌改别人的状态。
func TaskAgentOnMachine(ctx context.Context, ex Executor, taskID, machineID string) (bool, error) {
	var count int
	err := ex.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM task_agents ta
		JOIN agents a ON a.id = ta.agent_id
		WHERE ta.task_id = ? AND a.machine_id = ?`, taskID, machineID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("校验机器与任务的绑定: %w", err)
	}
	return count > 0, nil
}

// FindRepositoryByURL 按地址查找工作区已登记的仓库。
func FindRepositoryByURL(ctx context.Context, ex Executor, workspaceID, url string) (string, error) {
	var id string
	err := ex.QueryRowContext(ctx,
		`SELECT id FROM repositories WHERE workspace_id = ? AND url = ?`,
		workspaceID, strings.TrimSpace(url)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("查找仓库: %w", err)
	}
	return id, nil
}

// FindTaskByIdempotencyKey 按 CLI 幂等键查找已创建的任务（同一工作区）。
func FindTaskByIdempotencyKey(ctx context.Context, ex Executor, workspaceID, key string) (string, error) {
	var id string
	err := ex.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE workspace_id = ?
		  AND JSON_UNQUOTE(JSON_EXTRACT(config_snapshot, '$.cliIdempotencyKey')) = ?
		ORDER BY created_at ASC
		LIMIT 1`, workspaceID, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("按幂等键查找任务: %w", err)
	}
	return id, nil
}

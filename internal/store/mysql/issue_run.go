package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
)

// InsertIssueRun 写入一轮执行。
func InsertIssueRun(ctx context.Context, ex Executor, run domain.IssueRun) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO issue_runs (id, task_id, agent_id, session_id, execution_id, status,
			reply, error_class, detail, created_at, updated_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''),
			?, ?)`,
		run.ID, run.TaskID, run.AgentID, run.SessionID, run.ExecutionID, string(run.Status),
		run.Reply, run.ErrorClass, run.Detail, run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("写入执行记录: %w", err)
	}
	return nil
}

// GetIssueRunByExecution 按 execution_id 读取执行记录（daemon 回执反查用）。
func GetIssueRunByExecution(ctx context.Context, ex Executor, executionID string) (domain.IssueRun, error) {
	row := ex.QueryRowContext(ctx, `SELECT `+issueRunColumns+` FROM issue_runs WHERE execution_id = ?`,
		executionID)
	run, err := scanIssueRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.IssueRun{}, fmt.Errorf("读取执行记录: %w", err)
	}
	return run, nil
}

// LatestIssueRun 返回任务最近一轮执行；没有时返回空记录。
func LatestIssueRun(ctx context.Context, ex Executor, taskID string) (domain.IssueRun, bool, error) {
	row := ex.QueryRowContext(ctx, `SELECT `+issueRunColumns+` FROM issue_runs
		WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, taskID)
	run, err := scanIssueRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, false, nil
	}
	if err != nil {
		return domain.IssueRun{}, false, fmt.Errorf("读取最近执行: %w", err)
	}
	return run, true, nil
}

// ActiveIssueRun 返回仍在进行中的执行（dispatched/running）。
func ActiveIssueRun(ctx context.Context, ex Executor, taskID string) (domain.IssueRun, bool, error) {
	row := ex.QueryRowContext(ctx, `SELECT `+issueRunColumns+` FROM issue_runs
		WHERE task_id = ? AND status IN ('dispatched','running')
		ORDER BY created_at DESC LIMIT 1`, taskID)
	run, err := scanIssueRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, false, nil
	}
	if err != nil {
		return domain.IssueRun{}, false, fmt.Errorf("读取进行中的执行: %w", err)
	}
	return run, true, nil
}

// ListIssueRuns 返回任务的执行历史（新的在前）。
func ListIssueRuns(ctx context.Context, ex Executor, taskID string, limit int) ([]domain.IssueRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := ex.QueryContext(ctx, `SELECT `+issueRunColumns+` FROM issue_runs
		WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("列出执行记录: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.IssueRun
	for rows.Next() {
		run, err := scanIssueRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// FinishIssueRun 以 (status) 为条件写入终态。
func FinishIssueRun(ctx context.Context, ex Executor, id string, from []domain.RunStatus,
	to domain.RunStatus, reply, errorClass, detail string) (bool, error) {
	query := `UPDATE issue_runs SET status = ?, reply = NULLIF(?, ''),
		error_class = NULLIF(?, ''), detail = NULLIF(?, '')
		WHERE id = ?`
	args := []any{string(to), reply, errorClass, detail, id}
	// from 为空表示不限当前状态（调用方自己做前置校验）。
	if len(from) > 0 {
		query += " AND status IN ("
		for i, status := range from {
			if i > 0 {
				query += ", "
			}
			query += "?"
			args = append(args, string(status))
		}
		query += ")"
	}

	res, err := ex.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("更新执行状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// SetIssueRunUsage 写入这一轮的 token 用量与模型快照。
func SetIssueRunUsage(ctx context.Context, ex Executor, id string, input, output int64, model string) error {
	_, err := ex.ExecContext(ctx, `UPDATE issue_runs
		SET input_tokens = ?, output_tokens = ?, model = NULLIF(?, '')
		WHERE id = ?`, input, output, model, id)
	if err != nil {
		return fmt.Errorf("写入执行用量: %w", err)
	}
	return nil
}

// MarkRunning 把 dispatched 的执行推进为 running（首帧事件到达时）。
func MarkRunning(ctx context.Context, ex Executor, executionID string) error {
	_, err := ex.ExecContext(ctx,
		`UPDATE issue_runs SET status = 'running'
		  WHERE execution_id = ? AND status = 'dispatched'`, executionID)
	if err != nil {
		return fmt.Errorf("更新执行状态: %w", err)
	}
	return nil
}

const issueRunColumns = `id, task_id, agent_id, COALESCE(session_id, ''), execution_id,
	status, COALESCE(reply, ''), COALESCE(error_class, ''), COALESCE(detail, ''),
	input_tokens, output_tokens, COALESCE(model, ''), created_at, updated_at`

func scanIssueRun(row rowScanner) (domain.IssueRun, error) {
	var (
		run    domain.IssueRun
		status string
	)
	err := row.Scan(&run.ID, &run.TaskID, &run.AgentID, &run.SessionID, &run.ExecutionID,
		&status, &run.Reply, &run.ErrorClass, &run.Detail,
		&run.InputTokens, &run.OutputTokens, &run.Model, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return domain.IssueRun{}, err
	}
	run.Status = domain.RunStatus(status)
	return run, nil
}

// ListActiveRunsByMachine 列出某台机器上仍未收尾的执行（daemon 重启后用于清理）。
func ListActiveRunsByMachine(ctx context.Context, ex Executor, machineID string) ([]domain.IssueRun, error) {
	rows, err := ex.QueryContext(ctx, `SELECT `+issueRunColumns+`
		FROM issue_runs
		WHERE status IN ('dispatched','running')
		  AND agent_id IN (SELECT id FROM agents WHERE machine_id = ?)
		ORDER BY created_at ASC LIMIT 100`, machineID)
	if err != nil {
		return nil, fmt.Errorf("列出未收尾执行: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.IssueRun
	for rows.Next() {
		run, err := scanIssueRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

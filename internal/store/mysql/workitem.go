package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ErrWorkItemRunning 表示同一任务已有 running 工作项。
var ErrWorkItemRunning = errors.New("workitem: 同一 Issue 已有 running 工作项")

// InsertWorkItem 写入工作项。
func InsertWorkItem(ctx context.Context, ex Executor, item domain.WorkItem) error {
	if err := item.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO work_items (id, task_id, ordinal, owner_agent_id, status, version)
		VALUES (?, ?, ?, ?, ?, ?)`,
		item.ID, item.TaskID, item.Ordinal, item.OwnerAgentID, string(item.Status), item.Version)
	if err != nil {
		return fmt.Errorf("写入工作项: %w", err)
	}
	return nil
}

// GetWorkItem 读取工作项。
func GetWorkItem(ctx context.Context, ex Executor, id string) (domain.WorkItem, error) {
	var (
		item   domain.WorkItem
		status string
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, task_id, ordinal, owner_agent_id, status, version
		FROM work_items WHERE id = ?`, id).Scan(
		&item.ID, &item.TaskID, &item.Ordinal, &item.OwnerAgentID, &status, &item.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkItem{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.WorkItem{}, fmt.Errorf("读取工作项: %w", err)
	}
	item.Status = domain.WorkItemStatus(status)
	return item, nil
}

// ListWorkItems 按 ordinal 列出任务的工作项。
func ListWorkItems(ctx context.Context, ex Executor, taskID string) ([]domain.WorkItem, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT id, task_id, ordinal, owner_agent_id, status, version
		FROM work_items WHERE task_id = ? ORDER BY ordinal`, taskID)
	if err != nil {
		return nil, fmt.Errorf("列出工作项: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.WorkItem
	for rows.Next() {
		var (
			item   domain.WorkItem
			status string
		)
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Ordinal,
			&item.OwnerAgentID, &status, &item.Version); err != nil {
			return nil, err
		}
		item.Status = domain.WorkItemStatus(status)
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdateWorkItemStatus 以 CAS 更新状态。
func UpdateWorkItemStatus(ctx context.Context, ex Executor, id string,
	from, to domain.WorkItemStatus, expectedVersion uint64) (uint64, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE work_items SET status = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), id, string(from), expectedVersion)
	if err != nil {
		return 0, fmt.Errorf("更新工作项状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, errcode.New(errcode.Conflict, "工作项状态或版本已变化")
	}
	return expectedVersion + 1, nil
}

// StartWorkItem 把 ready 工作项置为 running，并保证同一任务只有一个 running。
//
// 先锁任务行再统计，避免两个并发启动同时通过检查。
func StartWorkItem(ctx context.Context, db *sql.DB, itemID string) (domain.WorkItem, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WorkItem{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		item   domain.WorkItem
		status string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, task_id, ordinal, owner_agent_id, status, version
		FROM work_items WHERE id = ? FOR UPDATE`, itemID).Scan(
		&item.ID, &item.TaskID, &item.Ordinal, &item.OwnerAgentID, &status, &item.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkItem{}, errcode.New(errcode.NotFound, "工作项不存在")
	}
	if err != nil {
		return domain.WorkItem{}, err
	}
	item.Status = domain.WorkItemStatus(status)

	var locked string
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM tasks WHERE id = ? FOR UPDATE`, item.TaskID).Scan(&locked); err != nil {
		return domain.WorkItem{}, err
	}
	var running int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM work_items WHERE task_id = ? AND status = 'running'`,
		item.TaskID).Scan(&running); err != nil {
		return domain.WorkItem{}, err
	}
	if running > 0 {
		return domain.WorkItem{}, ErrWorkItemRunning
	}
	if err := item.CanTransition(domain.WIStarted); err != nil {
		return domain.WorkItem{}, errcode.New(errcode.Conflict, err.Error())
	}
	if err := item.Apply(domain.WIStarted); err != nil {
		return domain.WorkItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE work_items SET status = 'running', version = version + 1
		WHERE id = ? AND status = 'ready' AND version = ?`, item.ID, item.Version-1); err != nil {
		return domain.WorkItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WorkItem{}, err
	}
	return item, nil
}

// NextWorkItemOrdinal 返回下一个序号（1 起）。
func NextWorkItemOrdinal(ctx context.Context, ex Executor, taskID string) (int, error) {
	var next sql.NullInt64
	if err := ex.QueryRowContext(ctx,
		`SELECT MAX(ordinal) FROM work_items WHERE task_id = ?`, taskID).Scan(&next); err != nil {
		return 0, err
	}
	if !next.Valid {
		return 1, nil
	}
	return int(next.Int64) + 1, nil
}

// SetWorkItemOwner 交接负责人。
func SetWorkItemOwner(ctx context.Context, ex Executor, id, agentID string) error {
	_, err := ex.ExecContext(ctx, `UPDATE work_items SET owner_agent_id = ? WHERE id = ?`, agentID, id)
	if err != nil {
		return fmt.Errorf("更新工作项负责人: %w", err)
	}
	return nil
}

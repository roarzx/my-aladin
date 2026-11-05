package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/domain"
)

// InsertFlowRun 写入流程运行。
func InsertFlowRun(ctx context.Context, ex Executor, run domain.FlowRun) error {
	if err := run.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO flow_runs (id, task_id, template_id, template_version,
			template_snapshot, inputs, status, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.TaskID, run.TemplateID, run.TemplateVersion,
		[]byte(run.TemplateSnapshot), []byte(run.Inputs), string(run.Status), run.Version)
	if err != nil {
		return fmt.Errorf("写入流程运行: %w", err)
	}
	return nil
}

// GetFlowRun 读取流程运行。
func GetFlowRun(ctx context.Context, ex Executor, id string) (domain.FlowRun, error) {
	var (
		run    domain.FlowRun
		status string
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, task_id, template_id, template_version, template_snapshot,
		       inputs, status, version
		FROM flow_runs WHERE id = ?`, id).Scan(
		&run.ID, &run.TaskID, &run.TemplateID, &run.TemplateVersion,
		&run.TemplateSnapshot, &run.Inputs, &status, &run.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FlowRun{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("读取流程运行: %w", err)
	}
	run.Status = domain.FlowRunStatus(status)
	return run, nil
}

// UpdateFlowRunStatus 以 CAS 更新运行状态。
func UpdateFlowRunStatus(ctx context.Context, ex Executor, id string,
	from, to domain.FlowRunStatus, expectedVersion uint64) (uint64, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE flow_runs SET status = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), id, string(from), expectedVersion)
	if err != nil {
		return 0, fmt.Errorf("更新流程状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, fmt.Errorf("流程状态或版本已变化")
	}
	return expectedVersion + 1, nil
}

// InsertFlowNodeRun 写入节点。
func InsertFlowNodeRun(ctx context.Context, ex Executor, node domain.FlowNodeRun) error {
	if err := node.Status.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO flow_node_runs (id, run_id, node_key, ordinal, status, attempt_no, lease_epoch)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.RunID, node.NodeKey, node.Ordinal, string(node.Status),
		node.AttemptNo, node.LeaseEpoch)
	if err != nil {
		return fmt.Errorf("写入流程节点: %w", err)
	}
	return nil
}

// ListFlowNodeRuns 按 ordinal 列出节点。
func ListFlowNodeRuns(ctx context.Context, ex Executor, runID string) ([]domain.FlowNodeRun, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT id, run_id, node_key, ordinal, status, attempt_no,
		       COALESCE(lease_owner, ''), lease_epoch, lease_expires_at, result
		FROM flow_node_runs WHERE run_id = ? ORDER BY ordinal`, runID)
	if err != nil {
		return nil, fmt.Errorf("列出流程节点: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.FlowNodeRun
	for rows.Next() {
		var (
			node   domain.FlowNodeRun
			status string
			result []byte
		)
		if err := rows.Scan(&node.ID, &node.RunID, &node.NodeKey, &node.Ordinal, &status,
			&node.AttemptNo, &node.LeaseOwner, &node.LeaseEpoch,
			&node.LeaseExpiresAt, &result); err != nil {
			return nil, err
		}
		node.Status = domain.FlowNodeStatus(status)
		node.Result = result
		out = append(out, node)
	}
	return out, rows.Err()
}

// GetFlowNodeRun 读取节点。
func GetFlowNodeRun(ctx context.Context, ex Executor, id string) (domain.FlowNodeRun, error) {
	var (
		node   domain.FlowNodeRun
		status string
		result []byte
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, run_id, node_key, ordinal, status, attempt_no,
		       COALESCE(lease_owner, ''), lease_epoch, lease_expires_at, result
		FROM flow_node_runs WHERE id = ?`, id).Scan(
		&node.ID, &node.RunID, &node.NodeKey, &node.Ordinal, &status, &node.AttemptNo,
		&node.LeaseOwner, &node.LeaseEpoch, &node.LeaseExpiresAt, &result)
	if err == nil {
		node.Result = result
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FlowNodeRun{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.FlowNodeRun{}, fmt.Errorf("读取流程节点: %w", err)
	}
	node.Status = domain.FlowNodeStatus(status)
	return node, nil
}

// UpdateFlowNodeStatus 以状态为条件更新节点，并可选写入结果。
func UpdateFlowNodeStatus(ctx context.Context, ex Executor, id string,
	from, to domain.FlowNodeStatus, result []byte) (bool, error) {
	res, err := ex.ExecContext(ctx, `
		UPDATE flow_node_runs SET status = ?, result = COALESCE(?, result)
		WHERE id = ? AND status = ?`,
		string(to), nullBytes(result), id, string(from))
	if err != nil {
		return false, fmt.Errorf("更新流程节点状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

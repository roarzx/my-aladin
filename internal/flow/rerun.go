package flow

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Rerun 重跑失败节点：节点回到 pending（attempt+1），下游节点标记 stale。
//
// 取消后的流程不允许重跑；done/succeeded 流程也不允许。
func Rerun(ctx context.Context, db *sql.DB, nodeID string) (domain.FlowNodeRun, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FlowNodeRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		node       domain.FlowNodeRun
		status     string
		runStatus  string
		runVer     uint64
		nodeResult []byte
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT n.id, n.run_id, n.node_key, n.ordinal, n.status, n.attempt_no,
		       COALESCE(n.lease_owner, ''), n.lease_epoch, n.lease_expires_at, n.result,
		       r.status, r.version
		FROM flow_node_runs n JOIN flow_runs r ON r.id = n.run_id
		WHERE n.id = ? FOR UPDATE`, nodeID).Scan(
		&node.ID, &node.RunID, &node.NodeKey, &node.Ordinal, &status, &node.AttemptNo,
		&node.LeaseOwner, &node.LeaseEpoch, &node.LeaseExpiresAt, &nodeResult,
		&runStatus, &runVer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.FlowNodeRun{}, errcode.New(errcode.NotFound, "节点不存在")
		}
		return domain.FlowNodeRun{}, err
	}
	node.Status = domain.FlowNodeStatus(status)
	node.Result = nodeResult
	if domain.FlowRunStatus(runStatus) == domain.FlowRunCancelled {
		return domain.FlowNodeRun{}, errcode.New(errcode.RunTerminal, "流程已取消，不能重跑")
	}
	if node.Status != domain.FlowNodeFailed {
		return domain.FlowNodeRun{}, errcode.Newf(errcode.Conflict,
			"只有 failed 节点可重跑（当前 %s）", node.Status)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE flow_node_runs SET status = 'pending', attempt_no = attempt_no + 1,
			lease_owner = NULL, lease_expires_at = NULL, result = NULL
		WHERE id = ?`, node.ID); err != nil {
		return domain.FlowNodeRun{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE flow_node_runs SET status = 'stale', lease_owner = NULL,
			lease_expires_at = NULL, result = NULL
		WHERE run_id = ? AND ordinal > ? AND status IN ('completed', 'failed', 'executing')`,
		node.RunID, node.Ordinal); err != nil {
		return domain.FlowNodeRun{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE flow_runs SET status = 'running', version = version + 1
		WHERE id = ? AND status IN ('failed', 'running', 'succeeded')`, node.RunID); err != nil {
		return domain.FlowNodeRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.FlowNodeRun{}, err
	}
	return domain.FlowNodeRun{
		ID: node.ID, RunID: node.RunID, NodeKey: node.NodeKey, Ordinal: node.Ordinal,
		Status: domain.FlowNodePending, AttemptNo: node.AttemptNo + 1,
	}, nil
}

// Cancel 取消流程；已终态的流程不可逆。
func Cancel(ctx context.Context, db *sql.DB, runID string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE flow_runs SET status = 'cancelled', version = version + 1
		WHERE id = ? AND status IN ('queued', 'running')`, runID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errcode.New(errcode.RunTerminal, "流程已结束或不存在")
	}
	return nil
}

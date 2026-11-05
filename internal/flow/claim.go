package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ErrNodeLeased 表示节点仍被其他机器持有。
var ErrNodeLeased = errors.New("flow: 节点已被其他机器持有")

// ErrLeaseLost 表示租约已失效或 epoch 不再是持有者。
var ErrLeaseLost = errors.New("flow: 节点租约已失效")

// ErrNodeNotReady 表示前序节点未完成或节点类型不可领取。
var ErrNodeNotReady = errors.New("flow: 节点尚不可领取")

// NodeResult 是机器上报的节点执行结果。
type NodeResult struct {
	NodeID    string
	MachineID string
	Epoch     uint64
	Success   bool
	Payload   json.RawMessage
}

// Claim 为机器领取下一个 agent_prompt 节点；过期租约可被重新领取。
func Claim(ctx context.Context, db *sql.DB, runID, machineID string,
	ttl time.Duration) (domain.FlowNodeRun, error) {
	if machineID == "" {
		return domain.FlowNodeRun{}, errcode.New(errcode.ValidationFailed, "machine_id 必填")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FlowNodeRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		runStatus string
		runIDGot  string
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT id, status FROM flow_runs WHERE id = ? FOR UPDATE`, runID).Scan(&runIDGot, &runStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.FlowNodeRun{}, errcode.New(errcode.NotFound, "流程运行不存在")
		}
		return domain.FlowNodeRun{}, err
	}
	status := domain.FlowRunStatus(runStatus)
	if status.IsTerminal() {
		return domain.FlowNodeRun{}, errcode.New(errcode.RunTerminal, "流程已结束")
	}
	if status == domain.FlowRunQueued {
		if _, err := tx.ExecContext(ctx,
			`UPDATE flow_runs SET status = 'running', version = version + 1 WHERE id = ?`, runID); err != nil {
			return domain.FlowNodeRun{}, err
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, run_id, node_key, ordinal, status, attempt_no,
		       COALESCE(lease_owner, ''), lease_epoch, lease_expires_at, result
		FROM flow_node_runs WHERE run_id = ? ORDER BY ordinal FOR UPDATE`, runID)
	if err != nil {
		return domain.FlowNodeRun{}, err
	}
	var nodes []domain.FlowNodeRun
	for rows.Next() {
		var (
			node   domain.FlowNodeRun
			st     string
			result []byte
		)
		if err := rows.Scan(&node.ID, &node.RunID, &node.NodeKey, &node.Ordinal, &st,
			&node.AttemptNo, &node.LeaseOwner, &node.LeaseEpoch,
			&node.LeaseExpiresAt, &result); err != nil {
			_ = rows.Close()
			return domain.FlowNodeRun{}, err
		}
		node.Status = domain.FlowNodeStatus(st)
		node.Result = result
		nodes = append(nodes, node)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return domain.FlowNodeRun{}, err
	}

	for _, node := range nodes {
		switch node.Status {
		case domain.FlowNodeCompleted, domain.FlowNodeSkipped:
			continue
		case domain.FlowNodeFailed:
			return domain.FlowNodeRun{}, errcode.New(errcode.Conflict, "前序节点失败，需先重跑")
		case domain.FlowNodeExecuting:
			if node.LeaseOwner != machineID && node.LeaseExpiresAt.Valid &&
				node.LeaseExpiresAt.Time.After(time.Now().UTC()) {
				return domain.FlowNodeRun{}, ErrNodeLeased
			}
		}
		if node.NodeKey != string(NodeAgentPrompt) {
			return domain.FlowNodeRun{}, ErrNodeNotReady
		}

		epoch := node.LeaseEpoch + 1
		expires := time.Now().UTC().Add(ttl)
		if _, err := tx.ExecContext(ctx, `
			UPDATE flow_node_runs SET status = 'executing', lease_owner = ?, lease_epoch = ?,
				lease_expires_at = ?
			WHERE id = ?`, machineID, epoch, expires, node.ID); err != nil {
			return domain.FlowNodeRun{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.FlowNodeRun{}, err
		}
		node.Status = domain.FlowNodeExecuting
		node.LeaseOwner = machineID
		node.LeaseEpoch = epoch
		node.LeaseExpiresAt = sql.NullTime{Time: expires, Valid: true}
		return node, nil
	}
	return domain.FlowNodeRun{}, errcode.New(errcode.Conflict, "没有可领取的节点")
}

// Renew 延长节点租约；owner/epoch 必须仍匹配且未过期。
func Renew(ctx context.Context, db *sql.DB, nodeID, machineID string,
	epoch uint64, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	res, err := db.ExecContext(ctx, `
		UPDATE flow_node_runs SET lease_expires_at = ?
		WHERE id = ? AND status = 'executing' AND lease_owner = ? AND lease_epoch = ?
		  AND lease_expires_at > UTC_TIMESTAMP(3)`,
		time.Now().UTC().Add(ttl), nodeID, machineID, epoch)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Accept 接收机器结果：校验 owner/epoch，取消后的流程不得被节点成功回调复活。
func Accept(ctx context.Context, db *sql.DB, result NodeResult) (domain.FlowNodeRun, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FlowNodeRun{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		node       domain.FlowNodeRun
		nodeStatus string
		runStatus  string
		nodeResult []byte
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT n.id, n.run_id, n.node_key, n.ordinal, n.status, n.attempt_no,
		       COALESCE(n.lease_owner, ''), n.lease_epoch, n.lease_expires_at, n.result,
		       r.status
		FROM flow_node_runs n JOIN flow_runs r ON r.id = n.run_id
		WHERE n.id = ? FOR UPDATE`, result.NodeID).Scan(
		&node.ID, &node.RunID, &node.NodeKey, &node.Ordinal, &nodeStatus, &node.AttemptNo,
		&node.LeaseOwner, &node.LeaseEpoch, &node.LeaseExpiresAt, &nodeResult,
		&runStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.FlowNodeRun{}, errcode.New(errcode.NotFound, "节点不存在")
		}
		return domain.FlowNodeRun{}, err
	}
	node.Status = domain.FlowNodeStatus(nodeStatus)
	node.Result = nodeResult
	if domain.FlowRunStatus(runStatus) == domain.FlowRunCancelled {
		return domain.FlowNodeRun{}, errcode.New(errcode.RunTerminal, "流程已取消，节点结果只记审计")
	}
	if node.Status != domain.FlowNodeExecuting || node.LeaseOwner != result.MachineID ||
		node.LeaseEpoch != result.Epoch {
		return domain.FlowNodeRun{}, ErrLeaseLost
	}

	to := domain.FlowNodeCompleted
	if !result.Success {
		to = domain.FlowNodeFailed
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE flow_node_runs SET status = ?, result = ?, lease_expires_at = NULL
		WHERE id = ?`, string(to), nullJSON(result.Payload), node.ID); err != nil {
		return domain.FlowNodeRun{}, err
	}
	node.Status = to
	node.Result = result.Payload

	if to == domain.FlowNodeFailed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE flow_runs SET status = 'failed', version = version + 1
			WHERE id = ? AND status = 'running'`, node.RunID); err != nil {
			return domain.FlowNodeRun{}, err
		}
	} else if err := finishIfComplete(ctx, tx, node.RunID); err != nil {
		return domain.FlowNodeRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.FlowNodeRun{}, err
	}
	return node, nil
}

// finishIfComplete 在全部节点完成时把运行置为 succeeded。
func finishIfComplete(ctx context.Context, tx *sql.Tx, runID string) error {
	var pending int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM flow_node_runs
		WHERE run_id = ? AND status NOT IN ('completed', 'skipped', 'stale')`,
		runID).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE flow_runs SET status = 'succeeded', version = version + 1
		WHERE id = ? AND status = 'running'`, runID)
	return err
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

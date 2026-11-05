package flow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// ErrNoClaimable 表示当前没有可领取的节点。
var ErrNoClaimable = errors.New("flow: 没有可领取的节点")

// ClaimNext 跨运行扫描可领取的 agent_prompt 节点（机器侧 Worker 使用）。
func ClaimNext(ctx context.Context, db *sql.DB, machineID string,
	ttl time.Duration) (domain.FlowNodeRun, domain.FlowRun, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id FROM flow_runs WHERE status IN ('queued', 'running')
		ORDER BY id LIMIT 50`)
	if err != nil {
		return domain.FlowNodeRun{}, domain.FlowRun{}, err
	}
	var runIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return domain.FlowNodeRun{}, domain.FlowRun{}, err
		}
		runIDs = append(runIDs, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return domain.FlowNodeRun{}, domain.FlowRun{}, err
	}

	for _, runID := range runIDs {
		node, err := Claim(ctx, db, runID, machineID, ttl)
		if err == nil {
			run, getErr := GetFlowRunByID(ctx, db, runID)
			if getErr != nil {
				return domain.FlowNodeRun{}, domain.FlowRun{}, getErr
			}
			return node, run, nil
		}
		if errors.Is(err, ErrNodeLeased) || errors.Is(err, ErrNodeNotReady) ||
			errors.Is(err, ErrNoClaimable) {
			continue
		}
		return domain.FlowNodeRun{}, domain.FlowRun{}, err
	}
	return domain.FlowNodeRun{}, domain.FlowRun{}, ErrNoClaimable
}

// GetFlowRunByID 读取流程运行。
func GetFlowRunByID(ctx context.Context, db *sql.DB, id string) (domain.FlowRun, error) {
	return mysqlstore.GetFlowRun(ctx, db, id)
}

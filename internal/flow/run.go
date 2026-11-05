package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type templateSnapshot struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    string     `json:"type"`
	Version int        `json:"version"`
	Nodes   []NodeSpec `json:"nodes"`
}

// CreateRun 创建验收运行：冻结模板版本与输入，并为固定节点建 pending 行。
func CreateRun(ctx context.Context, db *sql.DB, taskID, templateID string,
	inputs json.RawMessage) (domain.FlowRun, []domain.FlowNodeRun, error) {
	tpl, err := mysqlstore.GetFlowTemplate(ctx, db, templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FlowRun{}, nil, errcode.New(errcode.NotFound, "流程模板不存在")
	}
	if err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "读取模板失败")
	}
	if !tpl.Enabled {
		return domain.FlowRun{}, nil, errcode.New(errcode.ValidationFailed, "模板已停用")
	}
	var nodes []NodeSpec
	if err := json.Unmarshal(tpl.Nodes, &nodes); err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "模板节点损坏")
	}
	if err := ValidateNodes(nodes); err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.ValidationFailed, err.Error())
	}
	if len(inputs) == 0 {
		inputs = json.RawMessage(`{}`)
	}

	snapshot, err := json.Marshal(templateSnapshot{
		ID: tpl.ID, Name: tpl.Name, Type: tpl.Type, Version: tpl.Version, Nodes: nodes,
	})
	if err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.InternalError, "生成模板快照失败")
	}

	run := domain.FlowRun{
		ID: idgen.New(), TaskID: taskID, TemplateID: tpl.ID, TemplateVersion: tpl.Version,
		TemplateSnapshot: snapshot, Inputs: inputs, Status: domain.FlowRunQueued,
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "开启事务失败")
	}
	defer func() { _ = tx.Rollback() }()

	if err := mysqlstore.InsertFlowRun(ctx, tx, run); err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "写入运行失败")
	}
	nodeRuns := make([]domain.FlowNodeRun, 0, len(nodes))
	for i, node := range nodes {
		nr := domain.FlowNodeRun{
			ID: idgen.New(), RunID: run.ID, NodeKey: string(node.Key),
			Ordinal: i + 1, Status: domain.FlowNodePending, AttemptNo: 1,
		}
		if err := mysqlstore.InsertFlowNodeRun(ctx, tx, nr); err != nil {
			return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "写入节点失败")
		}
		nodeRuns = append(nodeRuns, nr)
	}
	if err := tx.Commit(); err != nil {
		return domain.FlowRun{}, nil, errcode.New(errcode.UpstreamUnavailable, "提交事务失败")
	}
	return run, nodeRuns, nil
}

// GetRun 读取运行与节点。
func GetRun(ctx context.Context, db *sql.DB, runID string) (domain.FlowRun, []domain.FlowNodeRun, error) {
	run, err := mysqlstore.GetFlowRun(ctx, db, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FlowRun{}, nil, errcode.New(errcode.NotFound, "流程运行不存在")
	}
	if err != nil {
		return domain.FlowRun{}, nil, err
	}
	nodes, err := mysqlstore.ListFlowNodeRuns(ctx, db, runID)
	if err != nil {
		return domain.FlowRun{}, nil, err
	}
	return run, nodes, nil
}

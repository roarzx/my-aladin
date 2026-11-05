// Package flow 实现验收流程模板、运行与节点调度（G1–G5）。
package flow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// NodeKey 是固定节点类型。
type NodeKey string

const (
	NodeEnvironmentPrepare NodeKey = "environment_prepare"
	NodeAgentPrompt        NodeKey = "agent_prompt"
	NodeMarsAcceptance     NodeKey = "mars_acceptance"
)

// fixedOrder 是 acceptance_test 的固定节点顺序，不可配置。
var fixedOrder = []NodeKey{NodeEnvironmentPrepare, NodeAgentPrompt, NodeMarsAcceptance}

// NodeSpec 是模板中的节点定义。
type NodeSpec struct {
	Key    NodeKey         `json:"key"`
	Config json.RawMessage `json:"config,omitempty"`
}

// Template 是验收流程模板。
type Template struct {
	ID          string
	WorkspaceID string
	Name        string
	Type        string
	Version     int
	Enabled     bool
	Nodes       []NodeSpec
}

// ValidateNodes 校验节点集合：必须恰好是固定顺序的三个节点。
func ValidateNodes(nodes []NodeSpec) error {
	if len(nodes) != len(fixedOrder) {
		return fmt.Errorf("acceptance_test 必须包含 %d 个固定节点", len(fixedOrder))
	}
	for i, key := range fixedOrder {
		if nodes[i].Key != key {
			return fmt.Errorf("第 %d 个节点必须是 %s（实际 %s）", i+1, key, nodes[i].Key)
		}
	}
	return nil
}

// DefaultNodes 返回固定节点定义。
func DefaultNodes() []NodeSpec {
	return []NodeSpec{
		{Key: NodeEnvironmentPrepare},
		{Key: NodeAgentPrompt},
		{Key: NodeMarsAcceptance},
	}
}

// Publish 发布新版本：expectedVersion 必须等于当前最新版本（首次为 0）。
//
// 每次发布插入新行（版本 +1），旧版本保留；返回新版本。
func Publish(ctx context.Context, db *sql.DB, workspaceID, name string,
	nodes []NodeSpec, expectedVersion int) (Template, error) {
	if err := ValidateNodes(nodes); err != nil {
		return Template{}, errcode.New(errcode.ValidationFailed, err.Error())
	}
	current, err := mysqlstore.LatestFlowTemplateVersion(ctx, db, workspaceID, name)
	if err != nil {
		return Template{}, errcode.New(errcode.UpstreamUnavailable, "读取模板版本失败")
	}
	if current != expectedVersion {
		return Template{}, errcode.Newf(errcode.RevisionConflict,
			"模板版本冲突: 当前 %d，期望 %d", current, expectedVersion)
	}
	tpl := Template{
		ID: idgen.New(), WorkspaceID: workspaceID, Name: name,
		Type: "acceptance_test", Version: current + 1, Enabled: true, Nodes: nodes,
	}
	if err := mysqlstore.InsertFlowTemplate(ctx, db, mysqlstore.FlowTemplate{
		ID: tpl.ID, WorkspaceID: tpl.WorkspaceID, Name: tpl.Name, Type: tpl.Type,
		Version: tpl.Version, Enabled: tpl.Enabled, Nodes: mustJSON(nodes),
	}); err != nil {
		return Template{}, errcode.New(errcode.UpstreamUnavailable, "写入模板失败")
	}
	return tpl, nil
}

// Disable 停用模板版本。
func Disable(ctx context.Context, db *sql.DB, templateID string) error {
	found, err := mysqlstore.SetFlowTemplateEnabled(ctx, db, templateID, false)
	if err != nil {
		return err
	}
	if !found {
		return errcode.New(errcode.NotFound, "模板不存在")
	}
	return nil
}

// Delete 删除模板版本；仅允许删除已停用的版本。
func Delete(ctx context.Context, db *sql.DB, templateID string) error {
	tpl, err := mysqlstore.GetFlowTemplate(ctx, db, templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "模板不存在")
	}
	if err != nil {
		return err
	}
	if tpl.Enabled {
		return errcode.New(errcode.Conflict, "删除前必须先停用模板")
	}
	if err := mysqlstore.DeleteFlowTemplate(ctx, db, templateID); err != nil {
		return err
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

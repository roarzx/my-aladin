package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// FlowTemplate 是 flow_templates 的一行。
type FlowTemplate struct {
	ID          string
	WorkspaceID string
	Name        string
	Type        string
	Version     int
	Enabled     bool
	Nodes       json.RawMessage
	CreatedAt   time.Time
}

// InsertFlowTemplate 写入模板版本。
func InsertFlowTemplate(ctx context.Context, ex Executor, tpl FlowTemplate) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO flow_templates (id, workspace_id, name, type, version, enabled, nodes)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tpl.ID, tpl.WorkspaceID, tpl.Name, tpl.Type, tpl.Version, tpl.Enabled, []byte(tpl.Nodes))
	if err != nil {
		return fmt.Errorf("写入流程模板: %w", err)
	}
	return nil
}

// LatestFlowTemplateVersion 返回某工作区同名模板的最新版本（0 表示没有）。
func LatestFlowTemplateVersion(ctx context.Context, ex Executor, workspaceID, name string) (int, error) {
	var version sql.NullInt64
	if err := ex.QueryRowContext(ctx, `
		SELECT MAX(version) FROM flow_templates WHERE workspace_id = ? AND name = ?`,
		workspaceID, name).Scan(&version); err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

// GetFlowTemplate 读取模板。
func GetFlowTemplate(ctx context.Context, ex Executor, id string) (FlowTemplate, error) {
	var (
		tpl     FlowTemplate
		enabled bool
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, type, version, enabled, nodes
		FROM flow_templates WHERE id = ?`, id).Scan(
		&tpl.ID, &tpl.WorkspaceID, &tpl.Name, &tpl.Type, &tpl.Version, &enabled, &tpl.Nodes)
	if errors.Is(err, sql.ErrNoRows) {
		return FlowTemplate{}, sql.ErrNoRows
	}
	if err != nil {
		return FlowTemplate{}, fmt.Errorf("读取流程模板: %w", err)
	}
	tpl.Enabled = enabled
	return tpl, nil
}

// SetFlowTemplateEnabled 启停模板；返回是否存在。
func SetFlowTemplateEnabled(ctx context.Context, ex Executor, id string, enabled bool) (bool, error) {
	res, err := ex.ExecContext(ctx, `UPDATE flow_templates SET enabled = ? WHERE id = ?`, enabled, id)
	if err != nil {
		return false, fmt.Errorf("更新模板状态: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// DeleteFlowTemplate 物理删除模板版本。
func DeleteFlowTemplate(ctx context.Context, ex Executor, id string) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM flow_templates WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除流程模板: %w", err)
	}
	return nil
}

// NewFlowTemplateID 生成模板 ID。
func NewFlowTemplateID() string { return idgen.New() }

package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// templateRef 是写进 config_snapshot 的模板引用。
type templateRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// expandTemplate 读取启用的模板并返回其 config；模板不存在或未启用返回 TEMPLATE_NOT_FOUND。
// templateID 为空时返回 (nil, nil, nil)，表示使用内置默认配置。
func expandTemplate(ctx context.Context, ex mysqlstore.Executor, workspaceID, templateID string) (*templateRef, json.RawMessage, error) {
	if templateID == "" {
		return nil, nil, nil
	}
	var (
		ref    templateRef
		config json.RawMessage
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, version, config
		FROM task_templates
		WHERE id = ? AND workspace_id = ? AND enabled = 1`,
		templateID, workspaceID).Scan(&ref.ID, &ref.Version, &config)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errcode.New(errcode.TemplateNotFound, "模板不存在或未启用")
	}
	if err != nil {
		return nil, nil, errcode.New(errcode.UpstreamUnavailable, "读取模板失败")
	}
	return &ref, config, nil
}

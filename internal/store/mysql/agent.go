package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/roarzx/my-aladin/internal/agent"
)

const agentColumns = `id, workspace_id, name, owner_id, visibility, runtime_type,
	machine_id, work_dir, model, thinking_level, system_prompt,
	capability_set, env_vars, enabled`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgent(row rowScanner) (agent.Agent, error) {
	var (
		a             agent.Agent
		visibility    string
		runtimeType   string
		model         sql.NullString
		thinkingLevel sql.NullString
		enabled       bool
	)
	err := row.Scan(
		&a.ID, &a.WorkspaceID, &a.Name, &a.OwnerID, &visibility, &runtimeType,
		&a.MachineID, &a.WorkDir, &model, &thinkingLevel, &a.SystemPrompt,
		&a.CapabilitySet, &a.EnvVars, &enabled,
	)
	if err != nil {
		return agent.Agent{}, err
	}
	a.Visibility = agent.Visibility(visibility)
	a.RuntimeType = agent.RuntimeType(runtimeType)
	a.Model = model.String
	a.ThinkingLevel = thinkingLevel.String
	a.Enabled = enabled
	return a, nil
}

// CreateAgent 写入 Agent 配置。
func CreateAgent(ctx context.Context, ex Executor, a agent.Agent) error {
	if err := a.Visibility.Valid(); err != nil {
		return err
	}
	var model, thinking any
	if a.Model != "" {
		model = a.Model
	}
	if a.ThinkingLevel != "" {
		thinking = a.ThinkingLevel
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO agents (id, workspace_id, name, owner_id, visibility, runtime_type,
			machine_id, work_dir, model, thinking_level, system_prompt,
			capability_set, env_vars, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkspaceID, a.Name, a.OwnerID, string(a.Visibility), string(a.RuntimeType),
		a.MachineID, a.WorkDir, model, thinking, a.SystemPrompt,
		a.CapabilitySet, a.EnvVars, a.Enabled)
	if err != nil {
		return fmt.Errorf("创建 Agent: %w", err)
	}
	return nil
}

// GetAgent 按 ID 读取 Agent；不存在返回 sql.ErrNoRows。
func GetAgent(ctx context.Context, ex Executor, id string) (agent.Agent, error) {
	row := ex.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Agent{}, sql.ErrNoRows
	}
	if err != nil {
		return agent.Agent{}, fmt.Errorf("读取 Agent: %w", err)
	}
	return a, nil
}

// ListSchedulable 返回 userID 可调度且启用的 Agent（B3 可见性规则）。
func ListSchedulable(ctx context.Context, ex Executor, workspaceID string, userID uint64) ([]agent.Agent, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT `+agentColumns+`
		FROM agents
		WHERE workspace_id = ? AND enabled = 1
		  AND (visibility = 'shared' OR owner_id = ?)
		ORDER BY name`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("列出可调度 Agent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []agent.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetVisibility 仅允许创建者修改可见性；否则返回 sql.ErrNoRows。
func SetVisibility(ctx context.Context, ex Executor, id string, ownerID uint64, visibility agent.Visibility) error {
	if err := visibility.Valid(); err != nil {
		return err
	}
	res, err := ex.ExecContext(ctx,
		`UPDATE agents SET visibility = ? WHERE id = ? AND owner_id = ?`,
		string(visibility), id, ownerID)
	if err != nil {
		return fmt.Errorf("修改可见性: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

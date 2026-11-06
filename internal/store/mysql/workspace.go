package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
)

// Workspace 是工作区记录。
type Workspace struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// CreateWorkspace 创建工作区。
func CreateWorkspace(ctx context.Context, ex Executor, ws Workspace) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO workspaces (id, name) VALUES (?, ?)`, ws.ID, ws.Name)
	if err != nil {
		return fmt.Errorf("创建 workspace: %w", err)
	}
	return nil
}

// AddMember 添加成员；重复添加由主键冲突拒绝。
func AddMember(ctx context.Context, ex Executor, workspaceID string, userID uint64, role domain.Role) error {
	if err := role.Valid(); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		workspaceID, userID, string(role))
	if err != nil {
		return fmt.Errorf("添加成员: %w", err)
	}
	return nil
}

// MemberRole 返回成员角色；非成员返回 sql.ErrNoRows（由调用方映射为 403）。
func MemberRole(ctx context.Context, ex Executor, workspaceID string, userID uint64) (domain.Role, error) {
	var raw string
	err := ex.QueryRowContext(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("查询成员角色: %w", err)
	}
	role := domain.Role(raw)
	if err := role.Valid(); err != nil {
		return "", fmt.Errorf("库中存在非法角色 %q: %w", raw, err)
	}
	return role, nil
}

// WorkspaceMembership 是用户加入的工作区及其角色。
type WorkspaceMembership struct {
	ID   string
	Name string
	Role domain.Role
}

// ListWorkspacesForUser 列出用户加入的工作区（按名称排序）。
func ListWorkspacesForUser(ctx context.Context, ex Executor, userID uint64) ([]WorkspaceMembership, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT w.id, w.name, m.role
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id = ?
		ORDER BY w.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("列出工作区: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkspaceMembership, 0, 8)
	for rows.Next() {
		var (
			ws   WorkspaceMembership
			role string
		)
		if err := rows.Scan(&ws.ID, &ws.Name, &role); err != nil {
			return nil, err
		}
		ws.Role = domain.Role(role)
		out = append(out, ws)
	}
	return out, rows.Err()
}

// WorkspaceStore 是 *sql.DB 之上的轻量仓储，供 HTTP 层做成员校验。
type WorkspaceStore struct {
	DB *sql.DB
}

// MemberRole 实现 httpapi.MemberLookup。
func (s *WorkspaceStore) MemberRole(ctx context.Context, workspaceID string, userID uint64) (domain.Role, error) {
	return MemberRole(ctx, s.DB, workspaceID, userID)
}

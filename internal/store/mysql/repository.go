package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Repository 是工作区内的代码仓库记录。
type Repository struct {
	ID            string
	WorkspaceID   string
	URL           string
	DefaultBranch string
	Provider      string
}

const repositoryColumns = `id, workspace_id, url, default_branch, provider`

// ErrRepositoryTaken 表示同一工作区内的仓库 URL 已被占用。
var ErrRepositoryTaken = errors.New("仓库 URL 已存在")

// CreateRepository 写入仓库；同工作区 URL 重复返回 ErrRepositoryTaken。
func CreateRepository(ctx context.Context, ex Executor, r Repository) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO repositories (id, workspace_id, url, default_branch, provider)
		VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.WorkspaceID, r.URL, r.DefaultBranch, r.Provider)
	if isDuplicateKey(err) {
		return ErrRepositoryTaken
	}
	if err != nil {
		return fmt.Errorf("创建仓库: %w", err)
	}
	return nil
}

// GetRepository 按 ID 读取仓库；不存在返回 sql.ErrNoRows。
func GetRepository(ctx context.Context, ex Executor, id string) (Repository, error) {
	row := ex.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE id = ?`, id)
	r, err := scanRepository(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, sql.ErrNoRows
	}
	if err != nil {
		return Repository{}, fmt.Errorf("读取仓库: %w", err)
	}
	return r, nil
}

// ListRepositoriesByWorkspace 列出工作区内仓库。
func ListRepositoriesByWorkspace(ctx context.Context, ex Executor, workspaceID string) ([]Repository, error) {
	rows, err := ex.QueryContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE workspace_id = ? ORDER BY url`,
		workspaceID)
	if err != nil {
		return nil, fmt.Errorf("列出仓库: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]Repository, 0, 8)
	for rows.Next() {
		r, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanRepository(row rowScanner) (Repository, error) {
	var r Repository
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.URL, &r.DefaultBranch, &r.Provider)
	return r, err
}

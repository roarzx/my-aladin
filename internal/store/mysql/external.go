package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// ExternalRequirementStore 实现来源 + URL 去重索引。
type ExternalRequirementStore struct{ DB *sql.DB }

// Lookup 查询已建任务。
func (s *ExternalRequirementStore) Lookup(ctx context.Context, source, url string) (string, bool, error) {
	var taskID string
	err := s.DB.QueryRowContext(ctx, `
		SELECT task_id FROM external_requirements WHERE source = ? AND url_hash = ?`,
		source, urlHash(url)).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询外部需求: %w", err)
	}
	return taskID, true, nil
}

// Save 写入去重记录。
func (s *ExternalRequirementStore) Save(ctx context.Context, source, url, taskID string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO external_requirements (source, url, url_hash, task_id)
		VALUES (?, ?, ?, ?)`, source, url, urlHash(url), taskID)
	if err != nil {
		return fmt.Errorf("写入外部需求: %w", err)
	}
	return nil
}

func urlHash(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

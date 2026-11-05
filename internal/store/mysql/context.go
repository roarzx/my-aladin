package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// ErrContextConflict 表示 expectedVersion 与当前最大版本不一致。
var ErrContextConflict = errors.New("context: 版本冲突")

// ContextEntry 是 context_entries 的一行。
type ContextEntry struct {
	TaskID      string
	ScopeKind   string
	ScopeID     string
	Kind        string
	Version     int
	Content     string
	ContentHash string
}

// CurrentContextVersion 返回当前最大版本；不存在时返回 0。
func CurrentContextVersion(ctx context.Context, ex Executor, taskID, scopeKind, scopeID, kind string) (int, error) {
	var version sql.NullInt64
	err := ex.QueryRowContext(ctx, `
		SELECT MAX(version) FROM context_entries
		WHERE task_id = ? AND scope_kind = ? AND scope_id = ? AND kind = ?`,
		taskID, scopeKind, scopeID, kind).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("查询上下文版本: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

// ListContextVersions 列出某个 scope 下全部 (kind, version)。
func ListContextVersions(ctx context.Context, ex Executor, taskID, scopeKind, scopeID string) ([]ContextEntry, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT kind, version FROM context_entries
		WHERE task_id = ? AND scope_kind = ? AND scope_id = ?
		ORDER BY kind, version`, taskID, scopeKind, scopeID)
	if err != nil {
		return nil, fmt.Errorf("列出上下文版本: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ContextEntry
	for rows.Next() {
		var entry ContextEntry
		entry.TaskID, entry.ScopeKind, entry.ScopeID = taskID, scopeKind, scopeID
		if err := rows.Scan(&entry.Kind, &entry.Version); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// GetContextVersion 读取指定版本。
func GetContextVersion(ctx context.Context, ex Executor, taskID, scopeKind, scopeID, kind string, version int) (ContextEntry, error) {
	var entry ContextEntry
	entry.TaskID, entry.ScopeKind, entry.ScopeID = taskID, scopeKind, scopeID
	entry.Kind, entry.Version = kind, version
	err := ex.QueryRowContext(ctx, `
		SELECT content, content_hash FROM context_entries
		WHERE task_id = ? AND scope_kind = ? AND scope_id = ? AND kind = ? AND version = ?`,
		taskID, scopeKind, scopeID, kind, version).Scan(&entry.Content, &entry.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ContextEntry{}, sql.ErrNoRows
	}
	if err != nil {
		return ContextEntry{}, fmt.Errorf("读取上下文版本: %w", err)
	}
	return entry, nil
}

// PutContextVersion 以 CAS 方式写入新版本（expectedVersion 必须等于当前版本）。
//
// 并发写者读到同一版本时，只有一个能插入 version+1，另一个触发唯一键冲突 → 版本冲突。
func PutContextVersion(ctx context.Context, ex Executor, taskID, scopeKind, scopeID, kind, content string, expectedVersion int) (ContextEntry, error) {
	current, err := CurrentContextVersion(ctx, ex, taskID, scopeKind, scopeID, kind)
	if err != nil {
		return ContextEntry{}, err
	}
	if current != expectedVersion {
		return ContextEntry{}, fmt.Errorf("%w: 当前 %d, 期望 %d", ErrContextConflict, current, expectedVersion)
	}

	sum := sha256.Sum256([]byte(content))
	entry := ContextEntry{
		TaskID: taskID, ScopeKind: scopeKind, ScopeID: scopeID, Kind: kind,
		Version: current + 1, Content: content, ContentHash: hex.EncodeToString(sum[:]),
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO context_entries (id, task_id, scope_kind, scope_id, kind, version, content, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		idgen.New(), taskID, scopeKind, scopeID, kind, entry.Version, content, entry.ContentHash)
	if err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ContextEntry{}, fmt.Errorf("%w: 并发写入", ErrContextConflict)
		}
		return ContextEntry{}, fmt.Errorf("写入上下文版本: %w", err)
	}
	return entry, nil
}

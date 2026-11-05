package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// CreateMachineToken 保存令牌哈希与有效期；明文不落库。
func CreateMachineToken(ctx context.Context, ex Executor, machineID, tokenHash string, expiresAt time.Time) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO machine_tokens (id, machine_id, token_hash, expires_at)
		VALUES (?, ?, ?, ?)`,
		idgen.New(), machineID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("创建机器令牌: %w", err)
	}
	return nil
}

// MachineByToken 用令牌哈希查找机器；过期、撤销或不存在都返回 sql.ErrNoRows。
func MachineByToken(ctx context.Context, ex Executor, tokenHash string) (string, error) {
	var machineID string
	err := ex.QueryRowContext(ctx, `
		SELECT machine_id FROM machine_tokens
		WHERE token_hash = ?
		  AND revoked_at IS NULL
		  AND expires_at > UTC_TIMESTAMP(3)`, tokenHash).Scan(&machineID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("查询机器令牌: %w", err)
	}
	return machineID, nil
}

// RevokeMachineToken 撤销令牌。
func RevokeMachineToken(ctx context.Context, ex Executor, tokenHash string) error {
	res, err := ex.ExecContext(ctx,
		`UPDATE machine_tokens SET revoked_at = UTC_TIMESTAMP(3) WHERE token_hash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("撤销机器令牌: %w", err)
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

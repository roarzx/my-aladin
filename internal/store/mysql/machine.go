package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/machine"
)

// UpsertMachine 注册或更新机器（B10）；注册即视为上线。
func UpsertMachine(ctx context.Context, ex Executor, m machine.Machine) error {
	caps, err := machine.EncodeCapabilities(m.Capabilities)
	if err != nil {
		return err
	}
	status := m.Status
	if status == "" {
		status = machine.StatusOnline
	}
	lastSeen := m.LastSeenAt
	if lastSeen == nil {
		now := time.Now().UTC()
		lastSeen = &now
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO machines (id, workspace_id, name, kind, status, capabilities, cli_version, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			capabilities = VALUES(capabilities),
			cli_version = VALUES(cli_version),
			last_seen_at = VALUES(last_seen_at)`,
		m.ID, m.WorkspaceID, m.Name, string(m.Kind), string(status), caps,
		nullString(m.CLIVersion), lastSeen)
	if err != nil {
		return fmt.Errorf("注册机器: %w", err)
	}
	return nil
}

// HeartbeatMachine 刷新心跳；机器不存在返回 sql.ErrNoRows。
func HeartbeatMachine(ctx context.Context, ex Executor, id string) error {
	res, err := ex.ExecContext(ctx,
		`UPDATE machines SET status = 'online', last_seen_at = ? WHERE id = ?`,
		time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("机器心跳: %w", err)
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

// GetMachine 读取机器记录。
func GetMachine(ctx context.Context, ex Executor, id string) (machine.Machine, error) {
	var (
		m        machine.Machine
		kind     string
		status   string
		rawCaps  json.RawMessage
		cliVer   sql.NullString
		lastSeen sql.NullTime
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, workspace_id, name, kind, status, capabilities, cli_version, last_seen_at
		FROM machines WHERE id = ?`, id).Scan(
		&m.ID, &m.WorkspaceID, &m.Name, &kind, &status, &rawCaps, &cliVer, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, sql.ErrNoRows
	}
	if err != nil {
		return machine.Machine{}, fmt.Errorf("读取机器: %w", err)
	}
	caps, err := machine.ParseCapabilities(rawCaps)
	if err != nil {
		return machine.Machine{}, err
	}
	m.Kind = machine.Kind(kind)
	m.Status = machine.Status(status)
	m.Capabilities = caps
	m.CLIVersion = cliVer.String
	if lastSeen.Valid {
		v := lastSeen.Time
		m.LastSeenAt = &v
	}
	return m, nil
}

// Supports 判断机器是否声明了能力（未声明的能力不下发对应命令）。
func Supports(ctx context.Context, ex Executor, id string, capability machine.Capability) (bool, error) {
	m, err := GetMachine(ctx, ex, id)
	if err != nil {
		return false, err
	}
	return m.Supports(capability), nil
}

// SetMachineStatus 修改机器状态。
func SetMachineStatus(ctx context.Context, ex Executor, id string, status machine.Status) error {
	res, err := ex.ExecContext(ctx, `UPDATE machines SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("修改机器状态: %w", err)
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

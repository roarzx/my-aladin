package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
)

const squadColumns = `id, workspace_id, name, leader_agent_id, member_ids, instructions,
	approval_policy, version, enabled, created_at, updated_at`

// CreateSquad 写入小队定义。
func CreateSquad(ctx context.Context, ex Executor, squad domain.Squad) error {
	members, err := json.Marshal(squad.MemberIDs)
	if err != nil {
		return err
	}
	policy, err := json.Marshal(squad.ApprovalPolicy)
	if err != nil {
		return err
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO squads (id, workspace_id, name, leader_agent_id, member_ids,
			instructions, approval_policy, version, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		squad.ID, squad.WorkspaceID, squad.Name, squad.LeaderAgentID, members,
		squad.Instructions, policy, squad.Version, squad.Enabled)
	if err != nil {
		return fmt.Errorf("写入小队: %w", err)
	}
	return nil
}

// GetSquad 读取小队定义。
func GetSquad(ctx context.Context, ex Executor, id string) (domain.Squad, error) {
	var (
		squad   domain.Squad
		members []byte
		policy  []byte
	)
	err := ex.QueryRowContext(ctx, `SELECT `+squadColumns+` FROM squads WHERE id = ?`, id).Scan(
		&squad.ID, &squad.WorkspaceID, &squad.Name, &squad.LeaderAgentID, &members,
		&squad.Instructions, &policy, &squad.Version, &squad.Enabled,
		&squad.CreatedAt, &squad.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Squad{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.Squad{}, fmt.Errorf("读取小队: %w", err)
	}
	if err := json.Unmarshal(members, &squad.MemberIDs); err != nil {
		return domain.Squad{}, fmt.Errorf("解析小队成员: %w", err)
	}
	if err := json.Unmarshal(policy, &squad.ApprovalPolicy); err != nil {
		return domain.Squad{}, fmt.Errorf("解析审批配置: %w", err)
	}
	return squad, nil
}

// UpdateSquad 更新定义并递增版本（expectedVersion 做乐观锁）。
func UpdateSquad(ctx context.Context, ex Executor, squad domain.Squad, expectedVersion uint64) error {
	members, err := json.Marshal(squad.MemberIDs)
	if err != nil {
		return err
	}
	policy, err := json.Marshal(squad.ApprovalPolicy)
	if err != nil {
		return err
	}
	res, err := ex.ExecContext(ctx, `
		UPDATE squads SET name = ?, leader_agent_id = ?, member_ids = ?, instructions = ?,
			approval_policy = ?, version = version + 1, enabled = ?
		WHERE id = ? AND version = ?`,
		squad.Name, squad.LeaderAgentID, members, squad.Instructions,
		policy, squad.Enabled, squad.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("更新小队: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("小队版本已变化")
	}
	return nil
}

// ListSquads 列出工作区的小队。
func ListSquads(ctx context.Context, ex Executor, workspaceID string) ([]domain.Squad, error) {
	rows, err := ex.QueryContext(ctx,
		`SELECT `+squadColumns+` FROM squads WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("列出小队: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Squad
	for rows.Next() {
		var (
			squad   domain.Squad
			members []byte
			policy  []byte
		)
		if err := rows.Scan(&squad.ID, &squad.WorkspaceID, &squad.Name, &squad.LeaderAgentID,
			&members, &squad.Instructions, &policy, &squad.Version, &squad.Enabled,
			&squad.CreatedAt, &squad.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(members, &squad.MemberIDs)
		_ = json.Unmarshal(policy, &squad.ApprovalPolicy)
		out = append(out, squad)
	}
	return out, rows.Err()
}

// InsertSquadSnapshot 冻结小队到任务；同一任务只能有一份快照。
func InsertSquadSnapshot(ctx context.Context, ex Executor, snap domain.SquadSnapshot) error {
	members, err := json.Marshal(snap.MemberIDs)
	if err != nil {
		return err
	}
	policy, err := json.Marshal(snap.ApprovalPolicy)
	if err != nil {
		return err
	}
	frozenAt := snap.FrozenAt
	if frozenAt.IsZero() {
		frozenAt = time.Now().UTC()
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO squad_snapshots (id, task_id, squad_id, leader_agent_id,
			member_ids, instructions, approval_policy, frozen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.TaskID, snap.SquadID, snap.LeaderAgentID, members,
		snap.Instructions, policy, frozenAt)
	if err != nil {
		return fmt.Errorf("写入小队快照: %w", err)
	}
	return nil
}

// GetSquadSnapshot 读取任务的冻结小队。
func GetSquadSnapshot(ctx context.Context, ex Executor, taskID string) (domain.SquadSnapshot, error) {
	var (
		snap    domain.SquadSnapshot
		members []byte
		policy  []byte
	)
	err := ex.QueryRowContext(ctx, `
		SELECT id, task_id, squad_id, leader_agent_id, member_ids, instructions,
		       approval_policy, frozen_at
		FROM squad_snapshots WHERE task_id = ?`, taskID).Scan(
		&snap.ID, &snap.TaskID, &snap.SquadID, &snap.LeaderAgentID, &members,
		&snap.Instructions, &policy, &snap.FrozenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SquadSnapshot{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.SquadSnapshot{}, fmt.Errorf("读取小队快照: %w", err)
	}
	_ = json.Unmarshal(members, &snap.MemberIDs)
	_ = json.Unmarshal(policy, &snap.ApprovalPolicy)
	return snap, nil
}

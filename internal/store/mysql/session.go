package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// SessionRecord 是 sessions 表的一行（D1）。
type SessionRecord struct {
	ID                string
	TaskID            string
	AgentID           string
	Kind              string
	Ordinal           int
	DispatchState     string
	MachineID         string
	ACPSessionID      string
	ResumeFingerprint string
	UpdatedAt         time.Time
}

const sessionColumns = `id, task_id, agent_id, kind, session_ordinal, dispatch_state,
	COALESCE(machine_id, ''), COALESCE(acp_session_id, ''), COALESCE(resume_fingerprint, ''), updated_at`

// EnsureSession 返回任务内某 Agent 的当前会话；不存在时创建（ordinal = 1）。
func EnsureSession(ctx context.Context, ex Executor, taskID, agentID, kind string) (SessionRecord, error) {
	record, err := findSession(ctx, ex, taskID, agentID, kind)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, err
	}

	record = SessionRecord{
		ID: idgen.New(), TaskID: taskID, AgentID: agentID, Kind: kind,
		Ordinal: 1, DispatchState: "waiting_agent",
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO sessions (id, task_id, agent_id, kind, session_ordinal, dispatch_state)
		VALUES (?, ?, ?, ?, ?, ?)`,
		record.ID, taskID, agentID, kind, record.Ordinal, record.DispatchState)
	if err != nil {
		// 并发创建：重新读取已有会话
		existing, findErr := findSession(ctx, ex, taskID, agentID, kind)
		if findErr == nil {
			return existing, nil
		}
		return SessionRecord{}, fmt.Errorf("创建会话: %w", err)
	}
	return record, nil
}

// FindSessionByKind 读取指定 task/agent/kind 的最新会话。
func FindSessionByKind(ctx context.Context, ex Executor, taskID, agentID, kind string) (SessionRecord, error) {
	return findSession(ctx, ex, taskID, agentID, kind)
}

func findSession(ctx context.Context, ex Executor, taskID, agentID, kind string) (SessionRecord, error) {
	var record SessionRecord
	err := ex.QueryRowContext(ctx, `
		SELECT `+sessionColumns+`
		FROM sessions
		WHERE task_id = ? AND agent_id = ? AND kind = ?
		ORDER BY session_ordinal DESC LIMIT 1`, taskID, agentID, kind).Scan(
		&record.ID, &record.TaskID, &record.AgentID, &record.Kind, &record.Ordinal,
		&record.DispatchState, &record.MachineID, &record.ACPSessionID,
		&record.ResumeFingerprint, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, sql.ErrNoRows
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("读取会话: %w", err)
	}
	return record, nil
}

// SetSessionDispatch 更新派发状态（waiting_agent / dispatch_failed / dispatched）。
func SetSessionDispatch(ctx context.Context, ex Executor, sessionID, state, machineID string) error {
	_, err := ex.ExecContext(ctx, `
		UPDATE sessions SET dispatch_state = ?, machine_id = ?, updated_at = UTC_TIMESTAMP(3)
		WHERE id = ?`, state, nullString(machineID), sessionID)
	if err != nil {
		return fmt.Errorf("更新会话派发状态: %w", err)
	}
	return nil
}

// GetPlannerAgentID 返回任务的唯一 Planner（活跃）。
func GetPlannerAgentID(ctx context.Context, ex Executor, taskID string) (string, error) {
	var agentID string
	err := ex.QueryRowContext(ctx, `
		SELECT agent_id FROM task_agents
		WHERE task_id = ? AND role = 'planner' AND active = 1 LIMIT 1`, taskID).Scan(&agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("读取 Planner: %w", err)
	}
	return agentID, nil
}

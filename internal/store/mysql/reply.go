package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// ReplyRecord 是 reply_activities 的一行（D4）。
type ReplyRecord struct {
	ID               string
	TaskID           string
	StepID           string
	SessionID        string
	SourceMessageID  string
	ExecutionAttempt uint32
	Phase            string
	CreatedAt        time.Time
	TerminalAt       *time.Time
}

const replyColumns = `id, task_id, COALESCE(step_id, ''), session_id, source_message_id,
	execution_attempt, phase, created_at, terminal_at`

// OpenReply 开启一次回复记录；同一 source_message 已有未终态记录时直接返回它。
func OpenReply(ctx context.Context, ex Executor, record ReplyRecord) (ReplyRecord, error) {
	existing, err := FindOpenReply(ctx, ex, record.SourceMessageID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ReplyRecord{}, err
	}

	if record.Phase == "" {
		record.Phase = "accepted"
	}
	if record.ID == "" {
		record.ID = idgen.New()
	}
	_, err = ex.ExecContext(ctx, `
		INSERT INTO reply_activities
			(id, task_id, step_id, session_id, source_message_id, execution_attempt, phase)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.TaskID, nullString(record.StepID), record.SessionID,
		record.SourceMessageID, record.ExecutionAttempt, record.Phase)
	if err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return FindOpenReply(ctx, ex, record.SourceMessageID)
		}
		return ReplyRecord{}, fmt.Errorf("创建回复记录: %w", err)
	}
	return record, nil
}

// FindOpenReply 读取同一消息的未终态回复。
func FindOpenReply(ctx context.Context, ex Executor, sourceMessageID string) (ReplyRecord, error) {
	var (
		record   ReplyRecord
		terminal sql.NullTime
	)
	err := ex.QueryRowContext(ctx, `
		SELECT `+replyColumns+`
		FROM reply_activities
		WHERE source_message_id = ? AND terminal_at IS NULL`, sourceMessageID).Scan(
		&record.ID, &record.TaskID, &record.StepID, &record.SessionID,
		&record.SourceMessageID, &record.ExecutionAttempt, &record.Phase,
		&record.CreatedAt, &terminal)
	if errors.Is(err, sql.ErrNoRows) {
		return ReplyRecord{}, sql.ErrNoRows
	}
	if err != nil {
		return ReplyRecord{}, fmt.Errorf("读取未终态回复: %w", err)
	}
	return record, nil
}

// CloseReply 关闭回复；重复关闭幂等。
func CloseReply(ctx context.Context, ex Executor, id, phase string) error {
	_, err := ex.ExecContext(ctx, `
		UPDATE reply_activities
		SET phase = ?, terminal_at = UTC_TIMESTAMP(3)
		WHERE id = ? AND terminal_at IS NULL`, phase, id)
	if err != nil {
		return fmt.Errorf("关闭回复: %w", err)
	}
	return nil
}

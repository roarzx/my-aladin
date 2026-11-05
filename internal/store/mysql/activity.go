package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/idgen"
)

// Activity 是 activities 表的一行。
type Activity struct {
	TaskID    string          `json:"taskId"`
	Seq       uint64          `json:"seq"`
	EventID   string          `json:"eventId"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

// AppendActivity 在同一事务内递增任务游标并写入事件（D13）。
func AppendActivity(ctx context.Context, db *sql.DB, taskID, kind string, payload any) (Activity, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Activity{}, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Activity{}, fmt.Errorf("开启事件事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET activity_seq = LAST_INSERT_ID(activity_seq + 1) WHERE id = ?`, taskID)
	if err != nil {
		return Activity{}, fmt.Errorf("分配事件序号: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Activity{}, err
	}
	if affected == 0 {
		return Activity{}, sql.ErrNoRows
	}

	var seq uint64
	if err := tx.QueryRowContext(ctx, `SELECT LAST_INSERT_ID()`).Scan(&seq); err != nil {
		return Activity{}, fmt.Errorf("读取事件序号: %w", err)
	}

	eventID := idgen.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO activities (task_id, seq, event_id, kind, payload)
		VALUES (?, ?, ?, ?, ?)`, taskID, seq, eventID, kind, raw); err != nil {
		return Activity{}, fmt.Errorf("写入事件: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Activity{}, fmt.Errorf("提交事件事务: %w", err)
	}

	return Activity{
		TaskID: taskID, Seq: seq, EventID: eventID,
		Kind: kind, Payload: raw, CreatedAt: time.Now().UTC(),
	}, nil
}

// ListActivities 返回 seq > after 的事件（游标语义为“大于”，允许有洞）。
func ListActivities(ctx context.Context, ex Executor, taskID string, after uint64, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := ex.QueryContext(ctx, `
		SELECT task_id, seq, event_id, kind, payload, created_at
		FROM activities
		WHERE task_id = ? AND seq > ?
		ORDER BY seq LIMIT ?`, taskID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("读取事件: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []Activity
	for rows.Next() {
		var event Activity
		if err := rows.Scan(&event.TaskID, &event.Seq, &event.EventID,
			&event.Kind, &event.Payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

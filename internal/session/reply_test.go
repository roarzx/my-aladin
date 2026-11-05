//go:build integration

package session_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/session"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestReplyLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	service := &session.ReplyService{DB: db}

	taskID, sessionID, source := idgen.New(), idgen.New(), idgen.New()
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM reply_activities WHERE task_id = ?`, taskID) })

	record := mysqlstore.ReplyRecord{
		TaskID: taskID, SessionID: sessionID,
		SourceMessageID: source, ExecutionAttempt: 1,
	}
	first, err := service.Open(ctx, record)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	again, err := service.Open(ctx, record)
	if err != nil || again.ID != first.ID {
		t.Fatalf("同一 source_message 只能有一个未终态 reply: %q vs %q, err=%v", first.ID, again.ID, err)
	}

	open, err := mysqlstore.FindOpenReply(ctx, db, source)
	if err != nil || open.ID != first.ID {
		t.Fatalf("FindOpenReply = %+v, err=%v", open, err)
	}

	if err := service.Close(ctx, first.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.FindOpenReply(ctx, db, source); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("关闭后不应再有未终态回复: %v", err)
	}
	if err := service.Close(ctx, first.ID, "completed"); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}

	second, err := service.Open(ctx, record)
	if err != nil || second.ID == first.ID {
		t.Fatalf("终态后应允许新回复: %+v, err=%v", second, err)
	}
}

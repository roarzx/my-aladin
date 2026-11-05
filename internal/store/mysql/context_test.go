//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestContextVersionCAS(t *testing.T) {
	tx := testkit.NewDB(t)
	ctx := context.Background()

	const taskID = "00000000-0000-0000-0000-00000000ee01"
	scope := "task"
	kind := "decisions"

	current, err := mysqlstore.CurrentContextVersion(ctx, tx, taskID, scope, taskID, kind)
	if err != nil || current != 0 {
		t.Fatalf("初始版本 = %d, err=%v", current, err)
	}

	entry, err := mysqlstore.PutContextVersion(ctx, tx, taskID, scope, taskID, kind, "v1", 0)
	if err != nil || entry.Version != 1 {
		t.Fatalf("写入 v1: %+v, err=%v", entry, err)
	}

	// 过期 expectedVersion → 冲突
	if _, err := mysqlstore.PutContextVersion(ctx, tx, taskID, scope, taskID, kind, "stale", 0); !errors.Is(err, mysqlstore.ErrContextConflict) {
		t.Fatalf("期望版本冲突，实际: %v", err)
	}

	entry, err = mysqlstore.PutContextVersion(ctx, tx, taskID, scope, taskID, kind, "v2", 1)
	if err != nil || entry.Version != 2 {
		t.Fatalf("写入 v2: %+v, err=%v", entry, err)
	}

	// D12：内容未变化时不升版本，直接返回当前版本
	same, err := mysqlstore.PutContextVersion(ctx, tx, taskID, scope, taskID, kind, "v2", 2)
	if err != nil || same.Version != 2 {
		t.Fatalf("相同内容写入: %+v, err=%v", same, err)
	}
	if current, err := mysqlstore.CurrentContextVersion(ctx, tx, taskID, scope, taskID, kind); err != nil || current != 2 {
		t.Fatalf("去重后版本 = %d, err=%v", current, err)
	}

	got, err := mysqlstore.GetContextVersion(ctx, tx, taskID, scope, taskID, kind, 2)
	if err != nil || got.Content != "v2" || got.ContentHash == "" {
		t.Fatalf("读取 v2: %+v, err=%v", got, err)
	}

	list, err := mysqlstore.ListContextVersions(ctx, tx, taskID, scope, taskID)
	if err != nil || len(list) != 2 {
		t.Fatalf("版本列表 = %+v, err=%v", list, err)
	}

	if _, err := mysqlstore.GetContextVersion(ctx, tx, taskID, scope, taskID, kind, 99); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在的版本应返回 ErrNoRows，实际: %v", err)
	}
}

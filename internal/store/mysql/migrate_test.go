//go:build integration

package mysql_test

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"

	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

// newDatabase 在测试服务器上创建一个独享库，测试结束删除。
func newDatabase(t *testing.T) string {
	t.Helper()
	base, err := driver.ParseDSN(testkit.MySQLDSN())
	if err != nil {
		t.Fatalf("解析 TEST_MYSQL_DSN: %v", err)
	}
	name := fmt.Sprintf("aladdin_mig_%d", time.Now().UnixNano())

	adminCfg := *base
	adminCfg.DBName = ""
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("打开 MySQL: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("MySQL 不可达（检查 TEST_MYSQL_DSN）: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("创建测试库: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		_ = admin.Close()
	})

	cfg := *base
	cfg.DBName = name
	return cfg.FormatDSN()
}

func TestMigrateUpDownUp(t *testing.T) {
	dsn := newDatabase(t)

	if err := mysqlstore.Migrate(dsn); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	defer db.Close()

	var tables int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()",
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables < 30 {
		t.Fatalf("表数量 = %d，期望 >= 30", tables)
	}

	// 唯一 Planner 必须由数据库强制（9.3 约束 3）
	const (
		wsID      = "00000000-0000-0000-0000-000000000001"
		taskID    = "00000000-0000-0000-0000-000000000002"
		plannerID = "00000000-0000-0000-0000-000000000003"
		memberID  = "00000000-0000-0000-0000-000000000004"
	)
	if _, err := db.Exec(
		`INSERT INTO workspaces (id, name) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE name = VALUES(name)`, wsID, "test"); err != nil {
		t.Fatalf("插入 workspace: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, created_by, name, config_snapshot)
		 VALUES (?, ?, 1, 't', JSON_OBJECT())`, taskID, wsID); err != nil {
		t.Fatalf("插入 task: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO task_agents (task_id, agent_ref, agent_id, role) VALUES (?, 'p1', ?, 'planner')`,
		taskID, plannerID); err != nil {
		t.Fatalf("插入 planner: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO task_agents (task_id, agent_ref, agent_id, role) VALUES (?, 'p2', ?, 'planner')`,
		taskID, memberID)
	var mysqlErr *driver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		t.Fatalf("第二个 planner 应触发 1062，实际: %v", err)
	}

	// down → up 幂等
	if err := mysqlstore.MigrateDown(dsn, 1); err != nil {
		t.Fatalf("回滚: %v", err)
	}
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'tasks'",
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("down 之后 tasks 表应不存在")
	}
	if err := mysqlstore.Migrate(dsn); err != nil {
		t.Fatalf("再次迁移: %v", err)
	}
	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("迁移版本 = %d，期望 1", version)
	}
}

func TestMigrateDownRejectsZero(t *testing.T) {
	if err := mysqlstore.MigrateDown("invalid", 0); err == nil {
		t.Fatal("steps=0 应报错")
	}
}

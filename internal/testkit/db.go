//go:build integration || e2e || perf

package testkit

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLDSN 返回集成测试使用的 DSN，可通过 TEST_MYSQL_DSN 覆盖。
func MySQLDSN() string {
	if v := os.Getenv("TEST_MYSQL_DSN"); v != "" {
		return v
	}
	return "root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC"
}

// NewDBRaw 返回真实连接池；测试结束自动关闭。
func NewDBRaw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", MySQLDSN())
	if err != nil {
		t.Fatalf("打开 MySQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("MySQL 不可达（检查 TEST_MYSQL_DSN=%s）: %v", MySQLDSN(), err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// NewDB 返回一个事务，测试结束自动回滚。
//
// 注意：MySQL 的 DDL 会隐式提交，需要建表/迁移的测试请用 NewDBRaw。
func NewDB(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := NewDBRaw(t).Begin()
	if err != nil {
		t.Fatalf("开启事务: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

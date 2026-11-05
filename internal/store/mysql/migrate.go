// Package mysql 提供 MySQL 仓储与迁移。
//
// 注意：MySQL 的 DDL 会隐式提交，因此迁移无法包在一个事务里；
// 版本记录在语句成功执行后写入 schema_migrations（见 dev_spec 9.3）。
package mysql

import (
	"database/sql"
	"fmt"
	"sort"

	_ "github.com/go-sql-driver/mysql"

	"github.com/roarzx/my-aladin/migrations"
)

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    BIGINT NOT NULL PRIMARY KEY,
  name       VARCHAR(255) NOT NULL,
  applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// Migrate 向前迁移到最新版本。
func Migrate(dsn string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("打开 MySQL: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("连接 MySQL: %w", err)
	}
	return migrateAll(db)
}

// MigrateDown 回滚最近 steps 个版本（steps 必须 >= 1）。
func MigrateDown(dsn string, steps int) error {
	if steps < 1 {
		return fmt.Errorf("steps 必须 >= 1")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("打开 MySQL: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("连接 MySQL: %w", err)
	}
	return migrateDown(db, steps)
}

func migrateAll(db *sql.DB) error {
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		return fmt.Errorf("创建 schema_migrations: %w", err)
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}
	all, err := migrations.All()
	if err != nil {
		return fmt.Errorf("读取迁移文件: %w", err)
	}
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		if err := execScript(db, m.Up); err != nil {
			return fmt.Errorf("迁移 %04d_%s 失败: %w", m.Version, m.Name, err)
		}
		if _, err := db.Exec(
			"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
			m.Version, m.Name,
		); err != nil {
			return fmt.Errorf("记录迁移 %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

func migrateDown(db *sql.DB, steps int) error {
	applied, err := appliedVersionList(db)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return nil
	}

	all, err := migrations.All()
	if err != nil {
		return fmt.Errorf("读取迁移文件: %w", err)
	}
	byVersion := make(map[int64]migrations.Migration, len(all))
	for _, m := range all {
		byVersion[m.Version] = m
	}

	sort.Slice(applied, func(i, j int) bool { return applied[i] > applied[j] })
	if steps > len(applied) {
		steps = len(applied)
	}
	for _, version := range applied[:steps] {
		m, ok := byVersion[version]
		if !ok {
			return fmt.Errorf("迁移版本 %d 已应用但找不到 down 脚本", version)
		}
		if m.Down == "" {
			return fmt.Errorf("迁移 %04d_%s 缺少 down 脚本", m.Version, m.Name)
		}
		if err := execScript(db, m.Down); err != nil {
			return fmt.Errorf("回滚 %04d_%s 失败: %w", m.Version, m.Name, err)
		}
		if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version); err != nil {
			return fmt.Errorf("删除迁移记录 %d: %w", version, err)
		}
	}
	return nil
}

func execScript(db *sql.DB, script string) error {
	for i, stmt := range splitSQL(script) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("第 %d 条语句: %w", i+1, err)
		}
	}
	return nil
}

func appliedVersions(db *sql.DB) (map[int64]bool, error) {
	list, err := appliedVersionList(db)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(list))
	for _, v := range list {
		out[v] = true
	}
	return out, nil
}

func appliedVersionList(db *sql.DB) ([]int64, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("读取 schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// Package health 实现 liveness 与 readiness 检查（A8）。
//
// liveness（/healthz）只表示进程活着；readiness（/readyz）要求 MySQL、Redis
// 可达且迁移版本不低于二进制期望值。
package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Dependency 是一个可被就绪检查的依赖项。
type Dependency interface {
	Name() string
	Check(ctx context.Context) error
}

// Checker 聚合多个依赖项。
type Checker struct {
	deps []Dependency
}

// New 构造一个 Checker。
func New(deps ...Dependency) *Checker {
	return &Checker{deps: deps}
}

// Check 返回所有失败依赖的合并错误；全部健康时返回 nil。
func (c *Checker) Check(ctx context.Context) error {
	var errs []error
	for _, dep := range c.deps {
		if err := dep.Check(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dep.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Details 返回每个失败依赖的原因，供 /readyz 输出。
func (c *Checker) Details(ctx context.Context) map[string]string {
	out := make(map[string]string)
	for _, dep := range c.deps {
		if err := dep.Check(ctx); err != nil {
			out[dep.Name()] = err.Error()
		}
	}
	return out
}

type mysqlDependency struct {
	db                *sql.DB
	expectedMigration int64
}

// MySQL 返回一个检查连通性与迁移版本的依赖项。
// expectedMigration <= 0 时跳过版本检查。
func MySQL(db *sql.DB, expectedMigration int64) Dependency {
	return &mysqlDependency{db: db, expectedMigration: expectedMigration}
}

func (d *mysqlDependency) Name() string { return "mysql" }

func (d *mysqlDependency) Check(ctx context.Context) error {
	if err := d.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	if d.expectedMigration <= 0 {
		return nil
	}
	var version sql.NullInt64
	if err := d.db.QueryRowContext(ctx,
		"SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("读取迁移版本: %w", err)
	}
	if !version.Valid {
		return fmt.Errorf("schema_migrations 为空")
	}
	if version.Int64 < d.expectedMigration {
		return fmt.Errorf("迁移版本落后: 当前 %d, 期望 %d", version.Int64, d.expectedMigration)
	}
	return nil
}

type redisDependency struct {
	client redis.Cmdable
}

// Redis 返回一个 Redis 连通性依赖项。
func Redis(client redis.Cmdable) Dependency {
	return &redisDependency{client: client}
}

func (d *redisDependency) Name() string { return "redis" }

func (d *redisDependency) Check(ctx context.Context) error {
	if err := d.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

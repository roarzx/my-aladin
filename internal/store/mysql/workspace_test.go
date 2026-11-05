//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	driver "github.com/go-sql-driver/mysql"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestWorkspaceMembership(t *testing.T) {
	tx := testkit.NewDB(t)
	ctx := context.Background()

	const (
		wsA   = "00000000-0000-0000-0000-00000000aa01"
		wsB   = "00000000-0000-0000-0000-00000000aa02"
		user  = uint64(42)
		owner = uint64(7)
	)

	if err := mysqlstore.CreateWorkspace(ctx, tx, mysqlstore.Workspace{ID: wsA, Name: "A"}); err != nil {
		t.Fatalf("创建 A: %v", err)
	}
	if err := mysqlstore.CreateWorkspace(ctx, tx, mysqlstore.Workspace{ID: wsB, Name: "B"}); err != nil {
		t.Fatalf("创建 B: %v", err)
	}
	if err := mysqlstore.AddMember(ctx, tx, wsA, owner, domain.RoleOwner); err != nil {
		t.Fatalf("添加 owner: %v", err)
	}
	if err := mysqlstore.AddMember(ctx, tx, wsA, user, domain.RoleMember); err != nil {
		t.Fatalf("添加 member: %v", err)
	}

	role, err := mysqlstore.MemberRole(ctx, tx, wsA, user)
	if err != nil || role != domain.RoleMember {
		t.Fatalf("角色 = %q, err = %v", role, err)
	}

	// 重复添加 → 1062
	err = mysqlstore.AddMember(ctx, tx, wsA, user, domain.RoleAdmin)
	var mysqlErr *driver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		t.Fatalf("重复添加应触发 1062，实际: %v", err)
	}

	// 跨工作区不可见
	if _, err := mysqlstore.MemberRole(ctx, tx, wsB, user); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("跨工作区应返回 ErrNoRows，实际: %v", err)
	}

	// 非法角色在写库前被拒绝
	if err := mysqlstore.AddMember(ctx, tx, wsA, 99, domain.Role("root")); err == nil {
		t.Fatal("非法角色应被拒绝")
	}
}

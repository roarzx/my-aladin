//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestMachineToken(t *testing.T) {
	tx := testkit.NewDB(t)
	ctx := context.Background()

	hash := machine.HashToken("secret-token")
	if err := mysqlstore.CreateMachineToken(ctx, tx, "machine-1", hash,
		time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("创建令牌: %v", err)
	}

	got, err := mysqlstore.MachineByToken(ctx, tx, hash)
	if err != nil || got != "machine-1" {
		t.Fatalf("查询令牌: %q, err=%v", got, err)
	}

	if _, err := mysqlstore.MachineByToken(ctx, tx, machine.HashToken("wrong")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("错误令牌应返回 ErrNoRows，实际: %v", err)
	}

	// 过期令牌不可用
	expiredHash := machine.HashToken("expired")
	if err := mysqlstore.CreateMachineToken(ctx, tx, "machine-2", expiredHash,
		time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.MachineByToken(ctx, tx, expiredHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("过期令牌应返回 ErrNoRows，实际: %v", err)
	}

	// 撤销后不可用
	if err := mysqlstore.RevokeMachineToken(ctx, tx, hash); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	if _, err := mysqlstore.MachineByToken(ctx, tx, hash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("撤销后应返回 ErrNoRows，实际: %v", err)
	}
}

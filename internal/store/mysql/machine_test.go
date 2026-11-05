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

func TestMachineRegistry(t *testing.T) {
	tx := testkit.NewDB(t)
	ctx := context.Background()

	const (
		wsID      = "00000000-0000-0000-0000-00000000dd01"
		machineID = "00000000-0000-0000-0000-00000000dd02"
	)

	if err := mysqlstore.CreateWorkspace(ctx, tx, mysqlstore.Workspace{ID: wsID, Name: "machines"}); err != nil {
		t.Fatal(err)
	}

	seen := time.Now().UTC().Add(-time.Minute)
	if err := mysqlstore.UpsertMachine(ctx, tx, machine.Machine{
		ID: machineID, WorkspaceID: wsID, Name: "dev-box", Kind: machine.KindLocal,
		Capabilities: []machine.Capability{machine.CapAttachments, machine.CapGit},
		CLIVersion:   "0.1.0",
		LastSeenAt:   &seen,
	}); err != nil {
		t.Fatalf("注册机器: %v", err)
	}

	got, err := mysqlstore.GetMachine(ctx, tx, machineID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != machine.StatusOnline || !got.Supports(machine.CapAttachments) || !got.Supports(machine.CapGit) {
		t.Fatalf("机器 = %+v", got)
	}

	supported, err := mysqlstore.Supports(ctx, tx, machineID, machine.CapFlowNode)
	if err != nil {
		t.Fatal(err)
	}
	if supported {
		t.Fatal("未声明 flow_node，不应支持")
	}

	// 心跳刷新 last_seen
	if err := mysqlstore.HeartbeatMachine(ctx, tx, machineID); err != nil {
		t.Fatal(err)
	}
	after, err := mysqlstore.GetMachine(ctx, tx, machineID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastSeenAt == nil || !after.LastSeenAt.After(seen) {
		t.Fatalf("心跳未刷新 last_seen: %+v", after.LastSeenAt)
	}

	// 状态切换
	if err := mysqlstore.SetMachineStatus(ctx, tx, machineID, machine.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	got, _ = mysqlstore.GetMachine(ctx, tx, machineID)
	if got.Status != machine.StatusDisabled {
		t.Fatalf("状态 = %q", got.Status)
	}

	// 再次注册更新能力（同一 ID）
	if err := mysqlstore.UpsertMachine(ctx, tx, machine.Machine{
		ID: machineID, WorkspaceID: wsID, Name: "dev-box", Kind: machine.KindLocal,
		Capabilities: []machine.Capability{machine.CapFlowNode},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = mysqlstore.GetMachine(ctx, tx, machineID)
	if !got.Supports(machine.CapFlowNode) || got.Supports(machine.CapAttachments) {
		t.Fatalf("能力未更新: %+v", got.Capabilities)
	}

	// 不存在的机器
	if err := mysqlstore.HeartbeatMachine(ctx, tx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("期望 ErrNoRows，实际: %v", err)
	}
}

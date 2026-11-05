//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/roarzx/my-aladin/internal/agent"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/testkit"
)

func TestAgentVisibilityAndScheduling(t *testing.T) {
	tx := testkit.NewDB(t)
	ctx := context.Background()

	const (
		wsID      = "00000000-0000-0000-0000-00000000bb01"
		machineID = "00000000-0000-0000-0000-00000000cc01"
	)

	if err := mysqlstore.CreateWorkspace(ctx, tx, mysqlstore.Workspace{ID: wsID, Name: "agents"}); err != nil {
		t.Fatalf("创建 workspace: %v", err)
	}

	makeAgent := func(id, name string, vis agent.Visibility, owner uint64, enabled bool) agent.Agent {
		return agent.Agent{
			ID: id, WorkspaceID: wsID, Name: name, OwnerID: owner,
			Visibility: vis, RuntimeType: agent.RuntimeClaudeCode,
			MachineID: machineID, WorkDir: "/tmp/work", SystemPrompt: "do the work",
			CapabilitySet: json.RawMessage(`{"skills":[]}`),
			EnvVars:       json.RawMessage(`{}`),
			Enabled:       enabled,
		}
	}

	private := makeAgent("00000000-0000-0000-0000-00000000bb11", "private", agent.Private, 1, true)
	shared := makeAgent("00000000-0000-0000-0000-00000000bb12", "shared", agent.Shared, 1, true)
	disabled := makeAgent("00000000-0000-0000-0000-00000000bb13", "disabled", agent.Shared, 1, false)
	for _, a := range []agent.Agent{private, shared, disabled} {
		if err := mysqlstore.CreateAgent(ctx, tx, a); err != nil {
			t.Fatalf("创建 Agent %s: %v", a.Name, err)
		}
	}

	// 其他用户只看得到 shared 且启用的
	list, err := mysqlstore.ListSchedulable(ctx, tx, wsID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != shared.ID {
		t.Fatalf("用户 2 可见 Agent = %+v", list)
	}

	// 创建者看得到自己的 private + shared，但看不到 disabled
	list, err = mysqlstore.ListSchedulable(ctx, tx, wsID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("用户 1 可见数量 = %d", len(list))
	}

	got, err := mysqlstore.GetAgent(ctx, tx, private.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerID != 1 || got.Visibility != agent.Private || !got.Enabled {
		t.Fatalf("读取结果 = %+v", got)
	}
	if got.MachineID != machineID || got.WorkDir != "/tmp/work" {
		t.Fatalf("字段丢失 = %+v", got)
	}

	// shared 转 private 后，其他用户派发必须失败（B3 验收）
	if err := mysqlstore.SetVisibility(ctx, tx, shared.ID, 1, agent.Private); err != nil {
		t.Fatalf("修改可见性: %v", err)
	}
	updated, err := mysqlstore.GetAgent(ctx, tx, shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := updated.SchedulableBy(2); err == nil {
		t.Fatal("共享转私有后，跨用户调度必须失败")
	}

	// 非创建者不能修改
	if err := mysqlstore.SetVisibility(ctx, tx, shared.ID, 2, agent.Shared); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("非创建者修改应返回 ErrNoRows，实际: %v", err)
	}
}

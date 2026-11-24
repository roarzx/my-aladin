//go:build integration

package stats_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/machine"
	"github.com/roarzx/my-aladin/internal/stats"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
)

// seedStatsFixture 建一个工作区/机器/Agent/Issue，并按需写入执行轮次与事件。
func seedStatsFixture(t *testing.T) (db *sql.DB, taskID, agentID string, cleanup func()) {
	t.Helper()
	db = testkit.NewDBRaw(t)
	ctx := context.Background()
	wsID, machineID, agentID := idgen.New(), idgen.New(), idgen.New()

	cleanup = func() {
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id = ?`, taskID)
		_, _ = db.Exec(`DELETE FROM task_agents WHERE agent_id = ?`, agentID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, taskID)
		_, _ = db.Exec(`DELETE FROM agents WHERE id = ?`, agentID)
		_, _ = db.Exec(`DELETE FROM machines WHERE id = ?`, machineID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	}
	t.Cleanup(cleanup)

	if err := mysqlstore.CreateWorkspace(ctx, db, mysqlstore.Workspace{ID: wsID, Name: "stats-" + wsID[:8]}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.UpsertMachine(ctx, db, machine.Machine{
		ID: machineID, WorkspaceID: wsID, Name: "stats-box", Kind: machine.KindLocal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: agentID, WorkspaceID: wsID, Name: "stats-agent", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode, MachineID: machineID,
		WorkDir: "/tmp", Model: "claude-sonnet-5-5", Enabled: true,
		CapabilitySet: json.RawMessage("[]"), EnvVars: json.RawMessage("{}"),
	}); err != nil {
		t.Fatal(err)
	}
	created, err := (&task.Service{DB: db}).Create(ctx, 1, wsID, task.CreateRequest{
		Name: "统计样本", Mode: domain.ModeOrdinary, AgentIDs: []string{agentID}, PlannerAgentID: agentID,
	})
	if err != nil {
		t.Fatalf("创建 Issue: %v", err)
	}
	return db, created.ID, agentID, cleanup
}

// insertRun 写一轮执行并收尾。
func insertRun(t *testing.T, db *sql.DB, taskID, agentID string,
	status domain.RunStatus, errorClass string, input, output int64) {
	t.Helper()
	ctx := context.Background()
	run := domain.IssueRun{
		ID: idgen.New(), TaskID: taskID, AgentID: agentID, ExecutionID: idgen.New(),
		Status: domain.RunDispatched, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := mysqlstore.InsertIssueRun(ctx, db, run); err != nil {
		t.Fatal(err)
	}
	if _, err := mysqlstore.FinishIssueRun(ctx, db, run.ID,
		[]domain.RunStatus{domain.RunDispatched}, status, "", errorClass, ""); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.SetIssueRunUsage(ctx, db, run.ID, input, output, "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
}

func appendEvent(t *testing.T, db *sql.DB, taskID, kind string, payload map[string]any) {
	t.Helper()
	if _, err := mysqlstore.AppendActivity(context.Background(), db, taskID, kind, payload); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewAggregatesRunsAndUsage(t *testing.T) {
	db, taskID, agentID, _ := seedStatsFixture(t)
	ctx := context.Background()

	// 基线：库里可能有别的数据，用差值断言。
	before, err := stats.New(db).Overview(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}

	insertRun(t, db, taskID, agentID, domain.RunCompleted, "", 2000, 200)
	insertRun(t, db, taskID, agentID, domain.RunFailed, "network", 1000, 100)
	appendEvent(t, db, taskID, "tool.call", map[string]any{
		"title": "ls -la /tmp", "status": "completed", "run_id": "r1",
	})
	appendEvent(t, db, taskID, "tool.call", map[string]any{
		"title": "ls /tmp", "status": "completed", "run_id": "r1",
	})
	appendEvent(t, db, taskID, "tool.call", map[string]any{
		"title": "ls /tmp", "status": "failed", "run_id": "r1",
	})
	appendEvent(t, db, taskID, "issue.status", map[string]any{"from": "executing", "to": "completed"})

	after, err := stats.New(db).Overview(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}

	runDelta := func(status string) int {
		return after.Runs.ByStatus[status] - before.Runs.ByStatus[status]
	}
	if runDelta("completed") != 1 || runDelta("failed") != 1 {
		t.Fatalf("轮次计数 = %+v", after.Runs.ByStatus)
	}
	if after.Runs.Finished-before.Runs.Finished != 2 {
		t.Fatalf("终态轮次差值 = %d", after.Runs.Finished-before.Runs.Finished)
	}
	if after.Tokens.Input-before.Tokens.Input != 3000 ||
		after.Tokens.Output-before.Tokens.Output != 300 {
		t.Fatalf("token 差值 = %d/%d", after.Tokens.Input-before.Tokens.Input,
			after.Tokens.Output-before.Tokens.Output)
	}
	// Issue 总数应等于表里的真实条数（基线在 fixture 建 Issue 之前拿不到）。
	var dbTotal int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&dbTotal); err != nil {
		t.Fatal(err)
	}
	if after.Issues.Total != dbTotal {
		t.Fatalf("Issue 总数 = %d，表里是 %d", after.Issues.Total, dbTotal)
	}

	toolCount := func(overview stats.Overview, name string) stats.NamedCount {
		for _, item := range overview.Tools {
			if item.Name == name {
				return item
			}
		}
		return stats.NamedCount{}
	}
	if got := toolCount(after, "ls").Count - toolCount(before, "ls").Count; got != 3 {
		t.Fatalf("ls 调用差值 = %d", got)
	}
	if got := toolCount(after, "ls").Failed - toolCount(before, "ls").Failed; got != 1 {
		t.Fatalf("ls 失败差值 = %d", got)
	}

	failureDelta := func(class string) int {
		sum := func(overview stats.Overview) int {
			for _, item := range overview.Failures {
				if item.Name == class {
					return item.Count
				}
			}
			return 0
		}
		return sum(after) - sum(before)
	}
	if got := failureDelta("network"); got != 1 {
		t.Fatalf("network 失败差值 = %d", got)
	}

	// 每日趋势要能算进这一轮。
	sumDaily := func(list []stats.DayTokens) int64 {
		var total int64
		for _, day := range list {
			total += day.Input
		}
		return total
	}
	if got := sumDaily(after.Tokens.Daily) - sumDaily(before.Tokens.Daily); got != 3000 {
		t.Fatalf("每日 token 差值 = %d", got)
	}

	// 模型归因取执行时的快照。
	found := false
	for _, item := range after.Tokens.ByModel {
		if item.Model == "claude-sonnet-5-5" {
			found = true
		}
	}
	if !found {
		t.Fatalf("模型拆分缺少样本: %+v", after.Tokens.ByModel)
	}

	text := stats.RenderPrometheus(after)
	for _, want := range []string{"aladdin_runs_total{", "aladdin_run_duration_seconds_bucket{", "aladdin_tokens_total{"} {
		if !strings.Contains(text, want) {
			t.Fatalf("指标输出缺少 %q", want)
		}
	}
	if strings.Contains(text, taskID) {
		t.Fatal("指标输出里出现了 task_id")
	}
}

func TestOverviewRejectsNothingOnEmptyDBWindow(t *testing.T) {
	db, _, _, _ := seedStatsFixture(t)
	// days=1 也要返回连续时间轴（一天一个点）。
	overview, err := stats.New(db).Overview(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Issues.Created) != 1 || len(overview.Tokens.Daily) != 1 {
		t.Fatalf("时间轴长度 = %d/%d", len(overview.Issues.Created), len(overview.Tokens.Daily))
	}
}

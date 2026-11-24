//go:build e2e

// EX-001 Baseline：执行权与迟到结果的故障注入。
//
// 本文件只加测试，不改生产代码；断言对应 docs/experiments/experiment-001.md §4。
// 运行：go test -tags=e2e ./tests/e2e/... -run 'TestRecovery' -count=1 -v
package e2e

import (
	"context"
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

func (e *e2eEnv) run(t *testing.T, executionID string) domain.IssueRun {
	t.Helper()
	run, err := mysqlstore.GetIssueRunByExecution(context.Background(), e.db, executionID)
	if err != nil {
		t.Fatalf("读取执行 %s: %v", executionID, err)
	}
	return run
}

func (e *e2eEnv) activities(t *testing.T, taskID string) []mysqlstore.Activity {
	t.Helper()
	items, err := mysqlstore.ListActivities(context.Background(), e.db, taskID, 0, 200)
	if err != nil {
		t.Fatalf("读取活动: %v", err)
	}
	return items
}

func countKind(items []mysqlstore.Activity, kind string) int {
	n := 0
	for _, item := range items {
		if item.Kind == kind {
			n++
		}
	}
	return n
}

func (e *e2eEnv) usage(t *testing.T, executionID string) (input, output int64) {
	t.Helper()
	run := e.run(t, executionID)
	return run.InputTokens, run.OutputTokens
}

// R1 断连重连：未收尾的执行被清理，Issue 不会被永久卡住。
func TestRecoveryR1StaleRunCleanup(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "R1 断连重连")

	run, err := env.service.StartTurn(ctx, taskID, "干活")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(`UPDATE issue_runs SET status = 'running' WHERE id = ?`, run.ID); err != nil {
		t.Fatal(err)
	}

	// 新连接到达：服务端判定上一批进程已不在。
	if failed := env.service.FailStaleRuns(ctx, env.machine); failed != 1 {
		t.Fatalf("R1: 应清理 1 条未收尾执行，实际 %d", failed)
	}
	got := env.run(t, run.ExecutionID)
	if got.Status != domain.RunFailed {
		t.Fatalf("R1: 执行状态 = %q，期望 failed", got.Status)
	}
	if got.ErrorClass != "runtime" {
		t.Fatalf("R1: error_class = %q，期望 runtime", got.ErrorClass)
	}
	if n := countKind(env.activities(t, taskID), "turn.failed"); n != 1 {
		t.Fatalf("R1: turn.failed 条数 = %d，期望 1", n)
	}
	// 清理后必须能重新开始一轮。
	if _, err := env.service.StartTurn(ctx, taskID, "再开一轮"); err != nil {
		t.Fatalf("R1: 清理后应能继续: %v", err)
	}
}

// R2 迟到 turn_done：run 已 failed 后到达的回执不得产生任何副作用。
func TestRecoveryR2LateTurnDoneAfterFail(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "R2 迟到完成回执")

	run, err := env.service.StartTurn(ctx, taskID, "干活")
	if err != nil {
		t.Fatal(err)
	}
	env.event(t, "turn_fail", run.ExecutionID, map[string]any{
		"reason": "进程被杀", "error_class": "runtime", "uncertain": true,
	})
	if got := env.run(t, run.ExecutionID); got.Status != domain.RunFailed {
		t.Fatalf("R2 前置: 执行状态 = %q，期望 failed", got.Status)
	}

	before := env.activities(t, taskID)
	// 迟到的成功回执。
	env.event(t, "turn_done", run.ExecutionID, map[string]any{
		"reply": "我其实做完了", "input_tokens": 100, "output_tokens": 50,
		"acp_session_id": "acp-late", "duration_ms": 900,
	})
	after := env.activities(t, taskID)

	if got := env.run(t, run.ExecutionID); got.Status != domain.RunFailed {
		t.Fatalf("R2: 迟到的 turn_done 改写了执行状态 → %q", got.Status)
	}
	if got := countKind(after, "message.assistant") - countKind(before, "message.assistant"); got != 0 {
		t.Errorf("R2 违规: 迟到回执写入了 %d 条 message.assistant（期望 0）", got)
	}
	if got := countKind(after, "turn.done") - countKind(before, "turn.done"); got != 0 {
		t.Errorf("R2 违规: 迟到回执写入了 %d 条 turn.done 活动（期望 0）", got)
	}
	if in, out := env.usage(t, run.ExecutionID); in != 0 || out != 0 {
		t.Errorf("R2 违规: 迟到回执写入了用量 input=%d output=%d（期望 0/0）", in, out)
	}
}

// R3 重复 turn_done：同一 run 的重复回执必须幂等。
func TestRecoveryR3DuplicateTurnDone(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "R3 重复完成回执")

	run, err := env.service.StartTurn(ctx, taskID, "干活")
	if err != nil {
		t.Fatal(err)
	}
	env.event(t, "turn_done", run.ExecutionID, map[string]any{
		"reply": "完成", "input_tokens": 10, "output_tokens": 5,
	})
	// 网络重试导致同一条回执再来一次（内容与用量都不同，便于识别覆盖）。
	env.event(t, "turn_done", run.ExecutionID, map[string]any{
		"reply": "重复的完成", "input_tokens": 100, "output_tokens": 50,
	})

	got := env.run(t, run.ExecutionID)
	if got.Status != domain.RunCompleted {
		t.Fatalf("R3: 执行状态 = %q，期望 completed", got.Status)
	}
	items := env.activities(t, taskID)
	if n := countKind(items, "message.assistant"); n != 1 {
		t.Errorf("R3 违规: message.assistant = %d 条（期望 1）", n)
	}
	if n := countKind(items, "turn.done"); n != 1 {
		t.Errorf("R3 违规: turn.done = %d 条（期望 1）", n)
	}
	if in, out := env.usage(t, run.ExecutionID); in != 10 || out != 5 {
		t.Errorf("R3 违规: 重复回执覆盖了用量 input=%d output=%d（期望 10/5）", in, out)
	}
}

// R4 迟到 turn_fail：run 已 completed 后到达的失败回执不得改写状态或污染活动流。
func TestRecoveryR4LateTurnFail(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "R4 迟到失败回执")

	run, err := env.service.StartTurn(ctx, taskID, "干活")
	if err != nil {
		t.Fatal(err)
	}
	env.event(t, "turn_done", run.ExecutionID, map[string]any{"reply": "完成"})
	if got := env.run(t, run.ExecutionID); got.Status != domain.RunCompleted {
		t.Fatalf("R4 前置: 执行状态 = %q，期望 completed", got.Status)
	}

	before := countKind(env.activities(t, taskID), "turn.failed")
	env.event(t, "turn_fail", run.ExecutionID, map[string]any{
		"reason": "迟到的失败", "error_class": "tool",
	})
	if got := env.run(t, run.ExecutionID); got.Status != domain.RunCompleted {
		t.Fatalf("R4: 迟到的 turn_fail 改写了执行状态 → %q", got.Status)
	}
	if n := countKind(env.activities(t, taskID), "turn.failed") - before; n != 0 {
		t.Errorf("R4 违规: 迟到的 turn_fail 写入了 %d 条 turn.failed（期望 0）", n)
	}
}

// R5 跨轮竞争：上一轮的迟到回执不得影响新一轮的终态与用量。
func TestRecoveryR5StaleAcrossTurns(t *testing.T) {
	env := setupIssue(t)
	ctx := context.Background()
	taskID := env.createIssue(t, "R5 跨轮迟到")

	first, err := env.service.StartTurn(ctx, taskID, "第一轮")
	if err != nil {
		t.Fatal(err)
	}
	if failed := env.service.FailStaleRuns(ctx, env.machine); failed != 1 {
		t.Fatalf("R5 前置: 应清理 1 条，实际 %d", failed)
	}

	second, err := env.service.StartTurn(ctx, taskID, "第二轮")
	if err != nil {
		t.Fatalf("R5 前置: 新一轮应可开始: %v", err)
	}
	env.event(t, "turn_done", second.ExecutionID, map[string]any{
		"reply": "第二轮完成", "input_tokens": 7, "output_tokens": 3,
	})
	if got := env.run(t, second.ExecutionID); got.Status != domain.RunCompleted {
		t.Fatalf("R5 前置: 第二轮状态 = %q，期望 completed", got.Status)
	}

	before := countKind(env.activities(t, taskID), "turn.done")
	env.event(t, "turn_done", first.ExecutionID, map[string]any{
		"reply": "第一轮迟到", "input_tokens": 999, "output_tokens": 999,
	})

	if got := env.run(t, first.ExecutionID); got.Status != domain.RunFailed {
		t.Fatalf("R5: 第一轮状态 = %q，期望 failed", got.Status)
	}
	if in, out := env.usage(t, second.ExecutionID); in != 7 || out != 3 {
		t.Errorf("R5: 第二轮用量被改写 input=%d output=%d（期望 7/3）", in, out)
	}
	if n := countKind(env.activities(t, taskID), "turn.done") - before; n != 0 {
		t.Errorf("R5 违规: 第一轮迟到回执写入了 %d 条 turn.done（期望 0）", n)
	}
	// 不能出现两条「进行中」的执行。
	active, err := mysqlstore.ListActiveRunsByMachine(ctx, env.db, env.machine)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Errorf("R5: 仍有 %d 条活跃执行，期望 0", len(active))
	}
}

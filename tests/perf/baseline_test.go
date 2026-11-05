//go:build perf

// Package perf 是性能与容量基线（I3）。
package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/agent"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/plan"
	aladdinruntime "github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/testkit"
)

type countingRuntime struct {
	mu    sync.Mutex
	count int
}

func (r *countingRuntime) Dispatch(_ context.Context, req aladdinruntime.DispatchRequest) (string, error) {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	return req.ExecutionID, nil
}

func (r *countingRuntime) Inspect(context.Context, string) (aladdinruntime.ExecutionSnapshot, error) {
	return aladdinruntime.ExecutionSnapshot{State: aladdinruntime.StateUnknown}, nil
}

func (r *countingRuntime) Cancel(context.Context, string) (bool, error) { return true, nil }

func (r *countingRuntime) Capabilities() aladdinruntime.CapabilitySet {
	return aladdinruntime.CapabilitySet{}
}

func TestBaseline(t *testing.T) {
	concurrency := envInt("PERF_N", 30)
	if concurrency < 1 || concurrency > 200 {
		t.Fatalf("PERF_N 超出范围: %d", concurrency)
	}
	ctx := context.Background()
	db := testkit.NewDBRaw(t)
	client := testkit.NewRedis(t)
	wsID, agentID := idgen.New(), idgen.New()
	if _, err := db.Exec(`INSERT INTO workspaces (id, name) VALUES (?, ?)`, wsID, "perf"); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.CreateAgent(ctx, db, agent.Agent{
		ID: agentID, WorkspaceID: wsID, Name: "perf", OwnerID: 1,
		Visibility: agent.Shared, RuntimeType: agent.RuntimeClaudeCode,
		MachineID: "machine-perf", WorkDir: "/tmp/perf", SystemPrompt: "p",
		CapabilitySet: json.RawMessage(`{}`), EnvVars: json.RawMessage(`{}`), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM attempts WHERE step_id IN (SELECT id FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)))`, wsID)
		_, _ = db.Exec(`DELETE FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?))`, wsID)
		_, _ = db.Exec(`DELETE FROM confirmations WHERE plan_id IN (SELECT id FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?))`, wsID)
		_, _ = db.Exec(`DELETE FROM plans WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM activities WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE task_id IN (SELECT id FROM tasks WHERE workspace_id = ?)`, wsID)
		_, _ = db.Exec(`DELETE FROM tasks WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM agents WHERE workspace_id = ?`, wsID)
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID)
	})
	taskIDs := make([]string, 0, concurrency)
	for i := 0; i < concurrency; i++ {
		created, err := (&task.Service{DB: db}).Create(ctx, 1, wsID, task.CreateRequest{
			Name: fmt.Sprintf("perf-%d", i), Mode: domain.ModeOrdinary,
			AgentIDs: []string{agentID}, PlannerAgentID: agentID,
		})
		if err != nil {
			t.Fatal(err)
		}
		planSvc := &plan.Service{DB: db}
		proposal, _, err := planSvc.Propose(ctx, created.ID, plan.Proposal{
			ContentMD: "# plan", Revision: 1,
			Steps: []plan.StepInput{{Ordinal: 1, Title: "s", Body: "b", AgentRef: "a1"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := planSvc.Confirm(ctx, proposal.ID, 1, proposal.ContentHash, 1); err != nil {
			t.Fatal(err)
		}
		current, _ := mysqlstore.GetTask(ctx, db, created.ID)
		if _, err := planSvc.Start(ctx, created.ID, current.Version); err != nil {
			t.Fatal(err)
		}
		taskIDs = append(taskIDs, created.ID)
	}

	runtimeStub := &countingRuntime{}
	lock := orchestration.NewTaskLock(client, 10*time.Second, -1)
	runner := &orchestration.PlanRunner{DB: db, Runtime: runtimeStub, Owner: "perf", Lock: lock}

	var wg sync.WaitGroup
	var mu sync.Mutex
	latencies := make([]time.Duration, 0, len(taskIDs))
	errors := make([]string, 0)
	start := time.Now()
	for _, taskID := range taskIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			begin := time.Now()
			if err := runner.RunOnce(ctx, id); err != nil {
				mu.Lock()
				errors = append(errors, err.Error())
				mu.Unlock()
				return
			}
			mu.Lock()
			latencies = append(latencies, time.Since(begin))
			mu.Unlock()
		}(taskID)
	}
	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	p50 := percentile(latencies, 50)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	t.Logf("N=%d elapsed=%v p50=%v p95=%v p99=%v errors=%d heap=%dMB dispatched=%d",
		concurrency, elapsed, p50, p95, p99, len(errors), mem.HeapAlloc>>20, runtimeStub.count)
	os.WriteFile("/tmp/perf-baseline.txt", []byte(fmt.Sprintf(
		"N=%d\nelapsed=%v\np50=%v\np95=%v\np99=%v\nerrors=%d\nheap=%dMB\ndispatched=%d\n",
		concurrency, elapsed, p50, p95, p99, len(errors), mem.HeapAlloc>>20, runtimeStub.count)), 0o600)

	if len(errors) > 0 {
		t.Fatalf("并发推进失败 %d 次: %v", len(errors), errors[:min(3, len(errors))])
	}
	if p95 > 5*time.Second {
		t.Fatalf("P95 超过 5s: %v", p95)
	}
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

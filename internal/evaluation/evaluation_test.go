package evaluation_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/roarzx/my-aladin/internal/evaluation"
)

type scriptedExecutor struct{}

func (scriptedExecutor) Execute(_ context.Context, task evaluation.TaskSpec) (evaluation.TaskResult, error) {
	switch task.ID {
	case "t1":
		return evaluation.TaskResult{
			TaskID: "t1", Success: true, Duration: 3 * time.Second,
			DurationKnown: true, Tokens: 100, TokensKnown: true,
		}, nil
	case "t2":
		return evaluation.TaskResult{
			TaskID: "t2", Success: false, Evidence: json.RawMessage(`{"log":"failed"}`),
			Duration: 1 * time.Second, DurationKnown: true,
		}, errors.New("step failed")
	case "t3":
		return evaluation.TaskResult{TaskID: "t3", Success: true, Recovered: true}, nil
	case "t4":
		return evaluation.TaskResult{
			TaskID: "t4", Success: true, DuplicateSideEffects: 1,
			Duration: 10 * time.Second, DurationKnown: true, TokensKnown: false,
		}, nil
	default:
		return evaluation.TaskResult{TaskID: task.ID}, nil
	}
}

func TestRunner(t *testing.T) {
	suite := evaluation.Suite{
		Name:  "smoke",
		Tasks: []evaluation.TaskSpec{{ID: "t1"}, {ID: "t2"}},
	}
	runner := &evaluation.Runner{
		Executor: scriptedExecutor{},
		Config:   evaluation.Config{Name: "baseline", Params: map[string]string{"model": "m1"}},
	}
	first, err := runner.Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigHash != second.ConfigHash || first.ConfigHash == "" {
		t.Fatalf("配置哈希不一致: %q / %q", first.ConfigHash, second.ConfigHash)
	}
	if len(first.Results) != 2 || first.Results[0].TaskID != "t1" {
		t.Fatalf("结果顺序 = %+v", first.Results)
	}
	// 失败任务保留原始证据
	if first.Results[1].Error == "" || len(first.Results[1].Evidence) == 0 {
		t.Fatalf("失败结果丢失证据: %+v", first.Results[1])
	}

	// 不同配置 → 哈希不同
	other := &evaluation.Runner{Executor: scriptedExecutor{},
		Config: evaluation.Config{Name: "baseline", Params: map[string]string{"model": "m2"}}}
	third, err := other.Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if third.ConfigHash == first.ConfigHash {
		t.Fatal("配置变化应改变哈希")
	}
}

func TestReport(t *testing.T) {
	report := &evaluation.Report{
		SuiteName: "smoke",
		Results: []evaluation.TaskResult{
			{TaskID: "t1", Success: true, Duration: 1 * time.Second, DurationKnown: true, Tokens: 10, TokensKnown: true},
			{TaskID: "t2", Success: false, Duration: 2 * time.Second, DurationKnown: true},
			{TaskID: "t3", Success: true, Recovered: true, Duration: 3 * time.Second, DurationKnown: true},
			{TaskID: "t4", Success: true, Duration: 4 * time.Second, DurationKnown: true, TokensKnown: false},
			{TaskID: "t5", Success: true, DuplicateSideEffects: 1, Duration: 10 * time.Second, DurationKnown: true},
			{TaskID: "t6", Success: true /* 缺失耗时与 token */},
		},
	}
	summary := report.Summarize()
	if summary.Total != 6 || summary.Succeeded != 5 || summary.Failed != 1 {
		t.Fatalf("计数 = %+v", summary)
	}
	if summary.DeliveryRate != 5.0/6.0 {
		t.Fatalf("交付率 = %v", summary.DeliveryRate)
	}
	if summary.RecoveryRate != 1.0/6.0 || summary.DuplicateSideEffectRate != 1.0/6.0 {
		t.Fatalf("恢复/重复率 = %+v", summary)
	}
	// 已知耗时 1,2,3,4,10 → median 3s（nearest-rank 第 3 个），P95 10s
	if summary.DurationMedian != 3*time.Second || summary.DurationP95 != 10*time.Second {
		t.Fatalf("耗时分位 = median %v, p95 %v", summary.DurationMedian, summary.DurationP95)
	}
	if summary.DurationMissing != 1 || summary.TokensMissing != 5 {
		t.Fatalf("缺失计数 = %+v", summary)
	}
	if summary.TokensTotal != 10 {
		t.Fatalf("tokens = %d", summary.TokensTotal)
	}

	// 空报告不 panic，且缺失字段为 0
	empty := (&evaluation.Report{}).Summarize()
	if empty.Total != 0 || empty.DurationMedian != 0 {
		t.Fatalf("空报告 = %+v", empty)
	}
}

func TestSuiteFixtureLoads(t *testing.T) {
	raw, err := os.ReadFile("../../tests/fixtures/evaluation/suite.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite evaluation.Suite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Name != "smoke" || len(suite.Tasks) != 2 {
		t.Fatalf("suite = %+v", suite)
	}
}

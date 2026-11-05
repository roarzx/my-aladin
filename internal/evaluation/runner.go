// Package evaluation 提供固定任务集重放与评估报告（H5/H6）。
package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// TaskSpec 是评估任务集中的一项。
type TaskSpec struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input,omitempty"`
	Expected json.RawMessage `json:"expected,omitempty"`
}

// Suite 是固定任务集。
type Suite struct {
	Name  string     `json:"name"`
	Tasks []TaskSpec `json:"tasks"`
}

// TaskResult 是单任务结果；缺失数据用 Known 标记，不补零。
type TaskResult struct {
	TaskID               string          `json:"taskId"`
	Success              bool            `json:"success"`
	Recovered            bool            `json:"recovered"`
	DuplicateSideEffects int             `json:"duplicateSideEffects"`
	Duration             time.Duration   `json:"duration"`
	DurationKnown        bool            `json:"durationKnown"`
	Tokens               int64           `json:"tokens"`
	TokensKnown          bool            `json:"tokensKnown"`
	Evidence             json.RawMessage `json:"evidence,omitempty"`
	Error                string          `json:"error,omitempty"`
}

// Executor 执行单个评估任务。
type Executor interface {
	Execute(ctx context.Context, task TaskSpec) (TaskResult, error)
}

// Config 是评估配置；参与配置哈希，保证同配置可对齐。
type Config struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}

// Runner 顺序重放任务集。
type Runner struct {
	Executor Executor
	Config   Config
}

// Report 是一次评估结果。
type Report struct {
	SuiteName  string       `json:"suiteName"`
	ConfigHash string       `json:"configHash"`
	CreatedAt  time.Time    `json:"createdAt"`
	Results    []TaskResult `json:"results"`
}

// Run 执行任务集；执行错误也写入结果并保留证据，不中断其余任务。
func (r *Runner) Run(ctx context.Context, suite Suite) (*Report, error) {
	if r.Executor == nil {
		return nil, errors.New("evaluation: Executor 未配置")
	}
	hash, err := configHash(suite, r.Config)
	if err != nil {
		return nil, err
	}
	report := &Report{
		SuiteName: suite.Name, ConfigHash: hash,
		CreatedAt: time.Now().UTC(), Results: make([]TaskResult, 0, len(suite.Tasks)),
	}
	for _, task := range suite.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := r.Executor.Execute(ctx, task)
		if result.TaskID == "" {
			result.TaskID = task.ID
		}
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func configHash(suite Suite, config Config) (string, error) {
	raw, err := json.Marshal(struct {
		Suite  Suite  `json:"suite"`
		Config Config `json:"config"`
	}{Suite: suite, Config: config})
	if err != nil {
		return "", fmt.Errorf("序列化评估配置: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// SortedDurations 返回已排序的已知耗时。
func (r *Report) SortedDurations() []time.Duration {
	out := make([]time.Duration, 0, len(r.Results))
	for _, result := range r.Results {
		if result.DurationKnown {
			out = append(out, result.Duration)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

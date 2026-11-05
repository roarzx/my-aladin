package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkspaceMetrics 是单工作区聚合。
type WorkspaceMetrics struct {
	Tasks         map[string]int64
	FailureByCls  map[string]int64
	StepDurations []time.Duration
	Tokens        int64
}

// Metrics 是进程内指标采集器（H4），按 workspace 维度聚合。
type Metrics struct {
	mu         sync.Mutex
	workspaces map[string]*WorkspaceMetrics
}

// NewMetrics 构造指标采集器。
func NewMetrics() *Metrics {
	return &Metrics{workspaces: make(map[string]*WorkspaceMetrics)}
}

func (m *Metrics) workspace(ws string) *WorkspaceMetrics {
	entry, ok := m.workspaces[ws]
	if !ok {
		entry = &WorkspaceMetrics{
			Tasks:        make(map[string]int64),
			FailureByCls: make(map[string]int64),
		}
		m.workspaces[ws] = entry
	}
	return entry
}

// IncTask 记录任务状态计数。
func (m *Metrics) IncTask(workspace, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspace(workspace).Tasks[status]++
}

// ObserveStepDuration 记录步骤耗时。
func (m *Metrics) ObserveStepDuration(workspace string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspace(workspace).StepDurations = append(m.workspace(workspace).StepDurations, duration)
}

// IncFailure 记录失败分类。
func (m *Metrics) IncFailure(workspace, class string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspace(workspace).FailureByCls[class]++
}

// AddTokens 记录 token 用量。
func (m *Metrics) AddTokens(workspace string, n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspace(workspace).Tokens += n
}

// Snapshot 返回按 workspace 聚合的深拷贝。
func (m *Metrics) Snapshot() map[string]WorkspaceMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]WorkspaceMetrics, len(m.workspaces))
	for ws, entry := range m.workspaces {
		copied := WorkspaceMetrics{
			Tasks:         make(map[string]int64, len(entry.Tasks)),
			FailureByCls:  make(map[string]int64, len(entry.FailureByCls)),
			StepDurations: append([]time.Duration(nil), entry.StepDurations...),
			Tokens:        entry.Tokens,
		}
		for key, value := range entry.Tasks {
			copied.Tasks[key] = value
		}
		for key, value := range entry.FailureByCls {
			copied.FailureByCls[key] = value
		}
		out[ws] = copied
	}
	return out
}

// Render 输出 Prometheus 文本格式（按 workspace 标签）。
func (m *Metrics) Render() string {
	snapshot := m.Snapshot()
	workspaces := make([]string, 0, len(snapshot))
	for ws := range snapshot {
		workspaces = append(workspaces, ws)
	}
	sort.Strings(workspaces)

	var builder strings.Builder
	for _, ws := range workspaces {
		entry := snapshot[ws]
		for status, count := range entry.Tasks {
			fmt.Fprintf(&builder, "aladdin_task_total{workspace=%q,status=%q} %d\n", ws, status, count)
		}
		for class, count := range entry.FailureByCls {
			fmt.Fprintf(&builder, "aladdin_failure_total{workspace=%q,error_class=%q} %d\n", ws, class, count)
		}
		fmt.Fprintf(&builder, "aladdin_tokens_total{workspace=%q} %d\n", ws, entry.Tokens)
		fmt.Fprintf(&builder, "aladdin_step_duration_count{workspace=%q} %d\n", ws, len(entry.StepDurations))
	}
	return builder.String()
}

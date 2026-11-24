// Package stats 把执行记录与事件聚合成监控数据（P0）。
//
// 数据全部来自 MySQL：issue_runs 提供每轮的状态、耗时、用量与 Agent 归属，
// activities 提供 Issue 状态变更与工具调用。这里不做常驻内存计数器，而是
// 按窗口查询并短缓存——重启不丢数，也不会因为重复事件重复计数。
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Service 按时间窗口聚合 Issue 与执行数据。
type Service struct {
	DB *sql.DB
	// TTL 是聚合结果的缓存时长；Prometheus 抓取通常 15–30s 一次。
	TTL time.Duration
	now func() time.Time

	mu       sync.Mutex
	cachedAt time.Time
	cached   *Overview
}

// New 构造聚合服务。
func New(db *sql.DB) *Service {
	return &Service{DB: db, TTL: 15 * time.Second, now: time.Now}
}

// Overview 是一次聚合的结果（前端看板与 /metrics 共用）。
type Overview struct {
	Days        int          `json:"days"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Issues      IssueStats   `json:"issues"`
	Runs        RunStats     `json:"runs"`
	Failures    []NamedCount `json:"failures"`
	Tools       []ToolStat   `json:"tools"`
	ToolsByDay  []DayCount   `json:"toolsByDay"`
	Tokens      TokenStats   `json:"tokens"`
	Machines    []NamedCount `json:"machines"`
	// Availability 是机器在线率（online / 全部登记机器）。
	Availability float64      `json:"availability"`
	Agents       []AgentStats `json:"agents"`
}

// IssueStats 是 Issue 层面的现状与每日趋势。
type IssueStats struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"byStatus"`
	// CompletionRate 是当前处于 completed 的 Issue 占比（Agent 自己上报的完成）。
	CompletionRate float64    `json:"completionRate"`
	Created        []DayCount `json:"created"`
	Completed      []DayCount `json:"completed"`
}

// RunStats 是执行轮次的成功率与耗时分布。
type RunStats struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"byStatus"`
	Finished    int            `json:"finished"`
	Succeeded   int            `json:"succeeded"`
	Failed      int            `json:"failed"`
	Cancelled   int            `json:"cancelled"`
	SuccessRate float64        `json:"successRate"`
	ErrorRate   float64        `json:"errorRate"`
	P50Ms       int64          `json:"p50Ms"`
	P95Ms       int64          `json:"p95Ms"`
	// Retried 是同一 Issue 的第 2 轮及以后的执行数（含用户追加消息）。
	Retried int `json:"retried"`
	// Durations 是各状态终态轮次的耗时样本（毫秒），渲染直方图用，不进 JSON。
	Durations map[string][]int64 `json:"-"`
}

// TokenStats 是按模型拆分的 token 用量与每日趋势。
type TokenStats struct {
	Input   int64         `json:"input"`
	Output  int64         `json:"output"`
	ByModel []ModelTokens `json:"byModel"`
	Daily   []DayTokens   `json:"daily"`
}

// ModelTokens 是单个模型的 token 用量。
type ModelTokens struct {
	Model  string `json:"model"`
	Input  int64  `json:"input"`
	Output int64  `json:"output"`
}

// DayTokens 是某一天的 token 用量。
type DayTokens struct {
	Date   string `json:"date"`
	Input  int64  `json:"input"`
	Output int64  `json:"output"`
}

// ToolStat 是某个工具的调用次数与耗时。
type ToolStat struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Failed int    `json:"failed"`
	P50Ms  int64  `json:"p50Ms"`
	P95Ms  int64  `json:"p95Ms"`
}

// AgentStats 是单个 Agent 在当前窗口内的执行画像。
type AgentStats struct {
	AgentID      string    `json:"agentId"`
	Name         string    `json:"name"`
	RuntimeType  string    `json:"runtimeType"`
	Model        string    `json:"model"`
	Runs         int       `json:"runs"`
	Finished     int       `json:"finished"`
	Succeeded    int       `json:"succeeded"`
	Failed       int       `json:"failed"`
	SuccessRate  float64   `json:"successRate"`
	P50Ms        int64     `json:"p50Ms"`
	P95Ms        int64     `json:"p95Ms"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	ToolCalls    int       `json:"toolCalls"`
	LastRunAt    time.Time `json:"lastRunAt"`
}

// DayCount 是某一天的计数。
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// NamedCount 是带名字的计数（失败分类、机器状态）。
type NamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Overview 返回最近 days 天的聚合结果（含缓存）。
func (s *Service) Overview(ctx context.Context, days int) (Overview, error) {
	days = clampDays(days)

	s.mu.Lock()
	cached := s.cached
	fresh := cached != nil && cached.Days == days && s.now().Sub(s.cachedAt) < s.TTL
	s.mu.Unlock()
	if fresh {
		return *cached, nil
	}

	overview, err := s.load(ctx, days)
	if err != nil {
		return Overview{}, err
	}
	s.mu.Lock()
	s.cached, s.cachedAt = &overview, s.now()
	s.mu.Unlock()
	return overview, nil
}

func clampDays(days int) int {
	if days <= 0 {
		return 7
	}
	if days > 90 {
		return 90
	}
	return days
}

func (s *Service) load(ctx context.Context, days int) (Overview, error) {
	now := s.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	since := today.AddDate(0, 0, -(days - 1))
	dates := make([]string, 0, days)
	for i := range days {
		dates = append(dates, since.AddDate(0, 0, i).Format("2006-01-02"))
	}

	overview := Overview{
		Days:        days,
		GeneratedAt: now,
		Issues:      IssueStats{ByStatus: map[string]int{}},
		Runs:        RunStats{ByStatus: map[string]int{}, Durations: map[string][]int64{}},
	}
	var err error
	if overview.Issues.ByStatus, overview.Issues.Total, err = s.issueStatus(ctx); err != nil {
		return Overview{}, err
	}
	if overview.Issues.Total > 0 {
		overview.Issues.CompletionRate =
			float64(overview.Issues.ByStatus["completed"]) / float64(overview.Issues.Total)
	}
	if overview.Issues.Created, err = s.dayCounts(ctx,
		`SELECT DATE_FORMAT(created_at, '%Y-%m-%d'), COUNT(*)
		   FROM tasks WHERE created_at >= ? GROUP BY 1`, since, dates); err != nil {
		return Overview{}, err
	}
	// 完成按事件算，不按当前状态算：一个 Issue 可以完成后再被打开。
	if overview.Issues.Completed, err = s.dayCounts(ctx,
		`SELECT DATE_FORMAT(a.created_at, '%Y-%m-%d'), COUNT(*)
		   FROM activities a JOIN tasks t ON t.id = a.task_id
		  WHERE a.kind = 'issue.status'
		    AND JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.to')) = 'completed'
		    AND a.created_at >= ? GROUP BY 1`, since, dates); err != nil {
		return Overview{}, err
	}
	// Issue 删除时 activities 不级联，所有事件查询都要 join tasks 把孤儿行排除。
	if overview.ToolsByDay, err = s.dayCounts(ctx,
		`SELECT DATE_FORMAT(a.created_at, '%Y-%m-%d'), COUNT(*)
		   FROM activities a JOIN tasks t ON t.id = a.task_id
		  WHERE a.kind = 'tool.call' AND a.created_at >= ? GROUP BY 1`, since, dates); err != nil {
		return Overview{}, err
	}
	if err := s.loadRuns(ctx, since, &overview); err != nil {
		return Overview{}, err
	}
	if err := s.loadTools(ctx, since, &overview); err != nil {
		return Overview{}, err
	}
	if err := s.loadTokens(ctx, since, dates, &overview); err != nil {
		return Overview{}, err
	}
	if overview.Machines, overview.Availability, err = s.machines(ctx); err != nil {
		return Overview{}, err
	}
	if err := s.loadAgents(ctx, since, &overview); err != nil {
		return Overview{}, err
	}
	return overview, nil
}

func (s *Service) issueStatus(ctx context.Context) (map[string]int, int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return nil, 0, fmt.Errorf("统计 Issue 状态: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byStatus := make(map[string]int)
	total := 0
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, 0, err
		}
		byStatus[status] = count
		total += count
	}
	return byStatus, total, rows.Err()
}

// dayCounts 按天统计，并把缺失的日期补零（图表需要连续时间轴）。
func (s *Service) dayCounts(ctx context.Context, query string, since time.Time, dates []string) ([]DayCount, error) {
	rows, err := s.DB.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("按天统计: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int, len(dates))
	for rows.Next() {
		var day string
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		counts[day] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]DayCount, 0, len(dates))
	for _, date := range dates {
		out = append(out, DayCount{Date: date, Count: counts[date]})
	}
	return out, nil
}

func (s *Service) loadRuns(ctx context.Context, since time.Time, overview *Overview) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT task_id, agent_id, status, COALESCE(error_class, ''),
		       TIMESTAMPDIFF(MICROSECOND, created_at, updated_at) DIV 1000,
		       input_tokens, output_tokens, created_at
		  FROM issue_runs WHERE created_at >= ?
		  ORDER BY created_at ASC`, since)
	if err != nil {
		return fmt.Errorf("统计执行轮次: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		all      []int64
		failures = make(map[string]int)
		seenTask = make(map[string]bool)
	)
	for rows.Next() {
		var (
			taskID, agentID, status, class string
			durMs                          int64
			input, output                  int64
			createdAt                      time.Time
		)
		if err := rows.Scan(&taskID, &agentID, &status, &class, &durMs,
			&input, &output, &createdAt); err != nil {
			return err
		}
		overview.Runs.Total++
		overview.Runs.ByStatus[status]++
		if seenTask[taskID] {
			overview.Runs.Retried++
		}
		seenTask[taskID] = true
		if class != "" {
			failures[class]++
		}
		// 只有终态轮次的 updated_at 才是结束时间；进行中的不计入时长。
		if !isTerminalRun(status) {
			continue
		}
		overview.Runs.Finished++
		switch status {
		case "completed":
			overview.Runs.Succeeded++
		case "failed":
			overview.Runs.Failed++
		case "cancelled":
			overview.Runs.Cancelled++
		}
		if durMs > 0 {
			overview.Runs.Durations[status] = append(overview.Runs.Durations[status], durMs)
			all = append(all, durMs)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if overview.Runs.Finished > 0 {
		overview.Runs.SuccessRate = float64(overview.Runs.Succeeded) / float64(overview.Runs.Finished)
		overview.Runs.ErrorRate = float64(overview.Runs.Failed) / float64(overview.Runs.Finished)
	}
	slices.Sort(all)
	overview.Runs.P50Ms = percentile(all, 50)
	overview.Runs.P95Ms = percentile(all, 95)
	overview.Failures = topN(failures, 6, "")
	return nil
}

func isTerminalRun(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Service) loadTools(ctx context.Context, since time.Time, overview *Overview) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.tool_kind')), ''),
		       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.title')), ''),
		       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.status')), ''),
		       CAST(COALESCE(JSON_EXTRACT(a.payload, '$.duration_ms'), 0) AS SIGNED)
		  FROM activities a JOIN tasks t ON t.id = a.task_id
		 WHERE a.kind = 'tool.call' AND a.created_at >= ?`, since)
	if err != nil {
		return fmt.Errorf("统计工具调用: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type aggregate struct {
		count  int
		failed int
		ms     []int64
	}
	byTool := make(map[string]*aggregate)
	for rows.Next() {
		var kind, title, status string
		var duration int64
		if err := rows.Scan(&kind, &title, &status, &duration); err != nil {
			return err
		}
		name := ToolName(kind, title)
		entry := byTool[name]
		if entry == nil {
			entry = &aggregate{}
			byTool[name] = entry
		}
		entry.count++
		if status == "failed" {
			entry.failed++
		}
		if duration > 0 {
			entry.ms = append(entry.ms, duration)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tools := make([]ToolStat, 0, len(byTool))
	for name, entry := range byTool {
		slices.Sort(entry.ms)
		tools = append(tools, ToolStat{
			Name:   name,
			Count:  entry.count,
			Failed: entry.failed,
			P50Ms:  percentile(entry.ms, 50),
			P95Ms:  percentile(entry.ms, 95),
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Count != tools[j].Count {
			return tools[i].Count > tools[j].Count
		}
		return tools[i].Name < tools[j].Name
	})
	if len(tools) > 8 {
		tools = tools[:8]
	}
	overview.Tools = tools
	return nil
}

// loadTokens 从 issue_runs 的用量快照聚合，同时产出模型拆分与每日趋势。
func (s *Service) loadTokens(ctx context.Context, since time.Time, dates []string, overview *Overview) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT DATE_FORMAT(created_at, '%Y-%m-%d'), COALESCE(model, ''),
		       input_tokens, output_tokens
		  FROM issue_runs WHERE created_at >= ?`, since)
	if err != nil {
		return fmt.Errorf("统计 token 用量: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byModel := make(map[string]*ModelTokens)
	byDay := make(map[string]*DayTokens)
	for rows.Next() {
		var day, model string
		var input, output int64
		if err := rows.Scan(&day, &model, &input, &output); err != nil {
			return err
		}
		if model == "" {
			model = "unknown"
		}
		entry := byModel[model]
		if entry == nil {
			entry = &ModelTokens{Model: model}
			byModel[model] = entry
		}
		entry.Input += input
		entry.Output += output

		daily := byDay[day]
		if daily == nil {
			daily = &DayTokens{Date: day}
			byDay[day] = daily
		}
		daily.Input += input
		daily.Output += output

		overview.Tokens.Input += input
		overview.Tokens.Output += output
	}
	if err := rows.Err(); err != nil {
		return err
	}

	models := make([]ModelTokens, 0, len(byModel))
	for _, entry := range byModel {
		models = append(models, *entry)
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].Input+models[i].Output > models[j].Input+models[j].Output
	})
	overview.Tokens.ByModel = models

	daily := make([]DayTokens, 0, len(dates))
	for _, date := range dates {
		if entry, ok := byDay[date]; ok {
			daily = append(daily, *entry)
			continue
		}
		daily = append(daily, DayTokens{Date: date})
	}
	overview.Tokens.Daily = daily
	return nil
}

// loadAgents 按 Agent 维度聚合执行轮次、耗时、token 与工具调用。
func (s *Service) loadAgents(ctx context.Context, since time.Time, overview *Overview) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT r.agent_id, COALESCE(ag.name, ''), COALESCE(ag.runtime_type, ''),
		       COALESCE(ag.model, ''), r.status,
		       TIMESTAMPDIFF(MICROSECOND, r.created_at, r.updated_at) DIV 1000,
		       r.input_tokens, r.output_tokens, r.created_at
		  FROM issue_runs r
		  LEFT JOIN agents ag ON ag.id = r.agent_id
		 WHERE r.created_at >= ?`, since)
	if err != nil {
		return fmt.Errorf("统计 Agent 执行: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byAgent := make(map[string]*AgentStats)
	durations := make(map[string][]int64)
	for rows.Next() {
		var (
			agentID, name, runtimeType, model, status string
			durMs, input, output                      int64
			createdAt                                 time.Time
		)
		if err := rows.Scan(&agentID, &name, &runtimeType, &model, &status,
			&durMs, &input, &output, &createdAt); err != nil {
			return err
		}
		entry := byAgent[agentID]
		if entry == nil {
			entry = &AgentStats{AgentID: agentID, Name: name, RuntimeType: runtimeType, Model: model}
			byAgent[agentID] = entry
		}
		entry.Runs++
		entry.InputTokens += input
		entry.OutputTokens += output
		if createdAt.After(entry.LastRunAt) {
			entry.LastRunAt = createdAt
		}
		if !isTerminalRun(status) {
			continue
		}
		entry.Finished++
		switch status {
		case "completed":
			entry.Succeeded++
		case "failed":
			entry.Failed++
		}
		if durMs > 0 {
			durations[agentID] = append(durations[agentID], durMs)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 工具调用按 run 归属到 Agent。
	toolRows, err := s.DB.QueryContext(ctx, `
		SELECT r.agent_id, COUNT(*)
		  FROM activities a
		  JOIN tasks t ON t.id = a.task_id
		  JOIN issue_runs r ON r.id = JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.run_id'))
		 WHERE a.kind = 'tool.call' AND a.created_at >= ?
		 GROUP BY r.agent_id`, since)
	if err != nil {
		return fmt.Errorf("统计 Agent 工具调用: %w", err)
	}
	defer func() { _ = toolRows.Close() }()
	for toolRows.Next() {
		var agentID string
		var count int
		if err := toolRows.Scan(&agentID, &count); err != nil {
			return err
		}
		if entry := byAgent[agentID]; entry != nil {
			entry.ToolCalls = count
		}
	}
	if err := toolRows.Err(); err != nil {
		return err
	}

	agents := make([]AgentStats, 0, len(byAgent))
	for id, entry := range byAgent {
		samples := durations[id]
		slices.Sort(samples)
		entry.P50Ms = percentile(samples, 50)
		entry.P95Ms = percentile(samples, 95)
		if entry.Finished > 0 {
			entry.SuccessRate = float64(entry.Succeeded) / float64(entry.Finished)
		}
		agents = append(agents, *entry)
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Runs != agents[j].Runs {
			return agents[i].Runs > agents[j].Runs
		}
		return agents[i].Name < agents[j].Name
	})
	overview.Agents = agents
	return nil
}

func (s *Service) machines(ctx context.Context) ([]NamedCount, float64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT status, COUNT(*) FROM machines GROUP BY status`)
	if err != nil {
		return nil, 0, fmt.Errorf("统计机器状态: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NamedCount
	total, online := 0, 0
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, 0, err
		}
		out = append(out, NamedCount{Name: status, Count: count})
		total += count
		if status == "online" {
			online = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if total == 0 {
		return out, 0, nil
	}
	return out, float64(online) / float64(total), nil
}

// percentile 用最近秩法计算分位数；样本为空时返回 0。
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// topN 取前 n 名，其余合并成 restName（为空则丢弃）。
func topN(counts map[string]int, n int, restName string) []NamedCount {
	names := make([]NamedCount, 0, len(counts))
	for name, count := range counts {
		names = append(names, NamedCount{Name: name, Count: count})
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].Count != names[j].Count {
			return names[i].Count > names[j].Count
		}
		return names[i].Name < names[j].Name
	})
	if len(names) <= n || restName == "" {
		if len(names) > n {
			return names[:n]
		}
		return names
	}
	rest := NamedCount{Name: restName}
	for _, item := range names[n:] {
		rest.Count += item.Count
	}
	return append(names[:n], rest)
}

// ToolName 从工具事件里取一个稳定的名字用于聚合。
//
// ACP 的 tool_kind 经常为空，标题则是命令本身（"ls -la /tmp/x"）；
// 取第一个词既能区分 Bash/Read/Edit 这类工具，也能区分常用命令。
func ToolName(kind, title string) string {
	name := strings.TrimSpace(kind)
	if name == "" {
		fields := strings.Fields(title)
		if len(fields) == 0 {
			return "unknown"
		}
		name = fields[0]
	}
	if runes := []rune(name); len(runes) > 24 {
		name = string(runes[:24])
	}
	return name
}

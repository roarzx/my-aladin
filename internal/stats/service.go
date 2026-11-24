// Package stats 把执行记录与事件聚合成监控数据（P0）。
//
// 数据全部来自 MySQL：issue_runs 提供每轮的状态、耗时与用量，
// activities 提供 Issue 状态变更与工具调用。这里不做常驻内存计数器，
// 而是按窗口查询并短缓存——重启不丢数，也不会因为重复事件重复计数。
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
	Tools       []NamedCount `json:"tools"`
	ToolsByDay  []DayCount   `json:"toolsByDay"`
	Tokens      TokenStats   `json:"tokens"`
	Machines    []NamedCount `json:"machines"`
}

// IssueStats 是 Issue 层面的现状与每日趋势。
type IssueStats struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"byStatus"`
	Created   []DayCount     `json:"created"`
	Completed []DayCount     `json:"completed"`
}

// RunStats 是执行轮次的成功率与耗时分布。
type RunStats struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"byStatus"`
	Finished    int            `json:"finished"`
	Succeeded   int            `json:"succeeded"`
	SuccessRate float64        `json:"successRate"`
	P50Ms       int64          `json:"p50Ms"`
	P95Ms       int64          `json:"p95Ms"`
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

// DayCount 是某一天的计数。
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// NamedCount 是带名字的计数（失败分类、工具名、机器状态）。
type NamedCount struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Failed int    `json:"failed,omitempty"`
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
	if overview.Machines, err = s.machines(ctx); err != nil {
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
		SELECT status, COALESCE(error_class, ''),
		       TIMESTAMPDIFF(MICROSECOND, created_at, updated_at) DIV 1000
		  FROM issue_runs WHERE created_at >= ?`, since)
	if err != nil {
		return fmt.Errorf("统计执行轮次: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var all []int64
	failures := make(map[string]int)
	for rows.Next() {
		var (
			status string
			class  string
			durMs  int64
		)
		if err := rows.Scan(&status, &class, &durMs); err != nil {
			return err
		}
		overview.Runs.Total++
		overview.Runs.ByStatus[status]++
		if class != "" {
			failures[class]++
		}
		// 只有终态轮次的 updated_at 才是结束时间；进行中的不计入时长。
		if !isTerminalRun(status) {
			continue
		}
		overview.Runs.Finished++
		if status == "completed" {
			overview.Runs.Succeeded++
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
		       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.payload, '$.status')), '')
		  FROM activities a JOIN tasks t ON t.id = a.task_id
		 WHERE a.kind = 'tool.call' AND a.created_at >= ?`, since)
	if err != nil {
		return fmt.Errorf("统计工具调用: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	failed := make(map[string]int)
	for rows.Next() {
		var kind, title, status string
		if err := rows.Scan(&kind, &title, &status); err != nil {
			return err
		}
		name := ToolName(kind, title)
		counts[name]++
		if status == "failed" {
			failed[name]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	overview.Tools = topN(counts, 8, "其他")
	for i := range overview.Tools {
		overview.Tools[i].Failed = failed[overview.Tools[i].Name]
	}
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

func (s *Service) machines(ctx context.Context) ([]NamedCount, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT status, COUNT(*) FROM machines GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("统计机器状态: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NamedCount
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out = append(out, NamedCount{Name: status, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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

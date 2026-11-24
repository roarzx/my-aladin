package stats

import (
	"fmt"
	"slices"
	"strings"
)

// durationBuckets 是执行轮次耗时的直方图边界（秒）。
//
// 覆盖从「几秒的小改动」到「几小时的大任务」，和 agent 默认 30 分钟单轮
// 上限对齐；桶边界一旦对外暴露就不要再改。
var durationBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400}

// RenderPrometheus 把聚合结果渲染成 Prometheus 文本格式。
//
// 只使用低基数标签（status / error_class / model / tool / agent）；
// task_id 与 run_id 这类高基数字段留在页面与事件里，不进指标。
func RenderPrometheus(overview Overview) string {
	var b strings.Builder

	writeHeader := func(name, help, kind string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}

	writeHeader("aladdin_issues", "当前 Issue 数量（按状态）", "gauge")
	for _, status := range sortedKeys(overview.Issues.ByStatus) {
		fmt.Fprintf(&b, "aladdin_issues{status=%q} %d\n", escapeLabel(status), overview.Issues.ByStatus[status])
	}

	writeHeader("aladdin_runs_total", "窗口内执行轮次（按状态）", "counter")
	for _, status := range sortedKeys(overview.Runs.ByStatus) {
		fmt.Fprintf(&b, "aladdin_runs_total{status=%q} %d\n", escapeLabel(status), overview.Runs.ByStatus[status])
	}

	writeHeader("aladdin_run_success_ratio", "终态轮次中上报完成的比例", "gauge")
	fmt.Fprintf(&b, "aladdin_run_success_ratio %.4f\n", overview.Runs.SuccessRate)

	writeHeader("aladdin_run_error_ratio", "终态轮次中失败的比例", "gauge")
	fmt.Fprintf(&b, "aladdin_run_error_ratio %.4f\n", overview.Runs.ErrorRate)

	writeHeader("aladdin_runs_retried_total", "同一 Issue 第 2 轮及以后的执行数（含追加消息）", "counter")
	fmt.Fprintf(&b, "aladdin_runs_retried_total %d\n", overview.Runs.Retried)

	writeHeader("aladdin_run_duration_seconds", "一轮执行的耗时分布（仅终态轮次）", "histogram")
	for _, status := range sortedKeys(overview.Runs.Durations) {
		renderHistogram(&b, status, overview.Runs.Durations[status])
	}

	writeHeader("aladdin_run_failures_total", "失败的执行轮次（按错误分类）", "counter")
	for _, item := range overview.Failures {
		fmt.Fprintf(&b, "aladdin_run_failures_total{error_class=%q} %d\n", escapeLabel(item.Name), item.Count)
	}

	writeHeader("aladdin_tool_calls_total", "工具调用次数（按工具与结果）", "counter")
	for _, item := range overview.Tools {
		ok := max(item.Count-item.Failed, 0)
		fmt.Fprintf(&b, "aladdin_tool_calls_total{tool=%q,status=\"ok\"} %d\n", escapeLabel(item.Name), ok)
		fmt.Fprintf(&b, "aladdin_tool_calls_total{tool=%q,status=\"failed\"} %d\n", escapeLabel(item.Name), item.Failed)
	}

	writeHeader("aladdin_tool_duration_seconds", "工具调用耗时分位（毫秒 → 秒）", "gauge")
	for _, item := range overview.Tools {
		// 只保留 p50/p95 两个分位，样本数即调用次数。
		fmt.Fprintf(&b, "aladdin_tool_duration_seconds{tool=%q,quantile=\"0.5\"} %s\n",
			escapeLabel(item.Name), formatSeconds(item.P50Ms))
		fmt.Fprintf(&b, "aladdin_tool_duration_seconds{tool=%q,quantile=\"0.95\"} %s\n",
			escapeLabel(item.Name), formatSeconds(item.P95Ms))
		fmt.Fprintf(&b, "aladdin_tool_duration_seconds_count{tool=%q} %d\n",
			escapeLabel(item.Name), item.Count)
	}

	writeHeader("aladdin_tokens_total", "模型 token 用量（每轮 Prompt 合计）", "counter")
	for _, item := range overview.Tokens.ByModel {
		fmt.Fprintf(&b, "aladdin_tokens_total{direction=\"input\",model=%q} %d\n", escapeLabel(item.Model), item.Input)
		fmt.Fprintf(&b, "aladdin_tokens_total{direction=\"output\",model=%q} %d\n", escapeLabel(item.Model), item.Output)
	}

	writeHeader("aladdin_agent_runs_total", "执行轮次（按 Agent 与状态）", "counter")
	for _, agent := range overview.Agents {
		label := agentLabel(agent)
		fmt.Fprintf(&b, "aladdin_agent_runs_total{agent=%q,runtime=%q,status=\"completed\"} %d\n",
			escapeLabel(label), escapeLabel(agent.RuntimeType), agent.Succeeded)
		fmt.Fprintf(&b, "aladdin_agent_runs_total{agent=%q,runtime=%q,status=\"failed\"} %d\n",
			escapeLabel(label), escapeLabel(agent.RuntimeType), agent.Failed)
		fmt.Fprintf(&b, "aladdin_agent_runs_total{agent=%q,runtime=%q,status=\"unfinished\"} %d\n",
			escapeLabel(label), escapeLabel(agent.RuntimeType), agent.Runs-agent.Finished)
	}

	writeHeader("aladdin_agent_tokens_total", "Agent token 用量", "counter")
	for _, agent := range overview.Agents {
		label := agentLabel(agent)
		fmt.Fprintf(&b, "aladdin_agent_tokens_total{agent=%q,direction=\"input\"} %d\n",
			escapeLabel(label), agent.InputTokens)
		fmt.Fprintf(&b, "aladdin_agent_tokens_total{agent=%q,direction=\"output\"} %d\n",
			escapeLabel(label), agent.OutputTokens)
	}

	writeHeader("aladdin_machines", "机器数量（按状态）", "gauge")
	for _, item := range overview.Machines {
		fmt.Fprintf(&b, "aladdin_machines{status=%q} %d\n", escapeLabel(item.Name), item.Count)
	}

	writeHeader("aladdin_machines_availability", "机器在线率", "gauge")
	fmt.Fprintf(&b, "aladdin_machines_availability %.4f\n", overview.Availability)

	writeHeader("aladdin_stats_generated_timestamp_seconds", "本次聚合的时间戳", "gauge")
	fmt.Fprintf(&b, "aladdin_stats_generated_timestamp_seconds %d\n", overview.GeneratedAt.Unix())

	return b.String()
}

// renderHistogram 输出一组累积桶、sum 与 count。
func renderHistogram(b *strings.Builder, status string, samples []int64) {
	// 样本是毫秒；桶边界是秒。
	seconds := make([]float64, 0, len(samples))
	var sum float64
	for _, ms := range samples {
		value := float64(ms) / 1000
		seconds = append(seconds, value)
		sum += value
	}
	slices.Sort(seconds)

	for _, bucket := range durationBuckets {
		count := 0
		for _, value := range seconds {
			if value <= bucket {
				count++
			}
		}
		fmt.Fprintf(b, "aladdin_run_duration_seconds_bucket{status=%q,le=%q} %d\n",
			escapeLabel(status), formatFloat(bucket), count)
	}
	fmt.Fprintf(b, "aladdin_run_duration_seconds_bucket{status=%q,le=\"+Inf\"} %d\n",
		escapeLabel(status), len(seconds))
	fmt.Fprintf(b, "aladdin_run_duration_seconds_sum{status=%q} %s\n",
		escapeLabel(status), formatFloat(sum))
	fmt.Fprintf(b, "aladdin_run_duration_seconds_count{status=%q} %d\n",
		escapeLabel(status), len(seconds))
}

// agentLabel 用「名字 + ID 前 8 位」做标签：同名 Agent 也不会混淆。
func agentLabel(agent AgentStats) string {
	short := agent.AgentID
	if len(short) > 8 {
		short = short[:8]
	}
	if agent.Name == "" {
		return short
	}
	return agent.Name + "/" + short
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// formatFloat 去掉浮点数末尾的零（60 → "60"，0.5 → "0.5"）。
func formatFloat(value float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.3f", value), "0"), ".")
}

// formatSeconds 把毫秒渲染成秒（保留三位小数）。
func formatSeconds(ms int64) string {
	return formatFloat(float64(ms) / 1000)
}

// escapeLabel 转义标签值里的反斜杠、引号与换行。
func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}

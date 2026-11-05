# I3 验收证据 — 性能与容量基线

实现：`tests/perf/baseline_test.go`；结果记录见
[`docs/perf-baseline.md`](../../../perf-baseline.md)。

场景：30 个普通任务并发调用 `PlanRunner.RunOnce`（真实 MySQL/Redis，内存 Runtime）。

```bash
make perf-test
# 或：PERF_N=30 go test -tags=perf ./tests/perf -run TestBaseline -v
```

本机结果（Apple M4 / Go 1.24.5 / MySQL 8 / Redis 7）：

| 指标 | 值 |
|---|---|
| N | 30 |
| 总耗时 | 70.1 ms |
| P50 / P95 / P99 | 68.8 / 70.0 / 70.0 ms |
| 失败 | 0 |
| 堆内存 | ~1 MB |

限制：不含真实 Agent 执行与流式回传，不代表生产容量。

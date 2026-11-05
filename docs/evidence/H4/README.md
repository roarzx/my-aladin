# H4 验收证据 — 指标采集

实现：`internal/observability/metrics.go`

- `IncTask` / `ObserveStepDuration` / `IncFailure` / `AddTokens`
- 按 workspace 聚合；`Snapshot()` 返回深拷贝；`Render()` 输出 Prometheus 文本（含 workspace 标签）

```bash
go test ./internal/observability -run TestMetrics -v
```

结果：不同 workspace 独立聚合；快照隔离；渲染包含 `aladdin_failure_total`、
`aladdin_tokens_total` 与 `workspace` 标签。

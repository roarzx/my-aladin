# H6 验收证据 — 评估报告

实现：`internal/evaluation/report.go`

- `Summarize` 输出交付率、恢复率、重复副作用率、token 总量
- 耗时给中位数与 P95（nearest-rank）；缺失耗时/token 显式计数，不补零
- 空报告返回零值而不 panic

```bash
go test ./internal/evaluation -run TestReport -v
```

结果：6 个任务交付率 5/6、恢复率 1/6、重复副作用率 1/6；
median=3s、P95=10s；DurationMissing=1、TokensMissing=5。

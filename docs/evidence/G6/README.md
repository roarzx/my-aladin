# G6 验收证据 — TAPD / Process 需求适配

实现：`internal/integration/{dedup,fetcher,tapd,process}.go`、
`internal/store/mysql/external.go`，迁移 `0005_external_requirements`

- `EnsureTask` 按 (source, url) 去重；已有任务直接复用，不重复抓取/建单
- `Sanitize` 去除控制字符、折叠空白、截断正文；`Requirement` 只有标题/正文/URL，
  不携带任何执行策略字段——来源正文不能覆盖执行配置
- TAPD / Process 抓取器使用注入的 HTTP Client，1MiB 响应上限

```bash
go test ./internal/integration -run 'TestDedup|TestSourceBodyCannotOverridePolicy' -v
```

结果：重复事件不建任务；不同来源同 URL 独立；不支持来源报错；正文中的伪策略
只作为文本保留（无执行影响）；标题控制字符被清理、正文截断到 8KiB。

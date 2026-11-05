# H2 验收证据 — Trace 关联

实现：`internal/observability/trace.go`

- `StartSpan(ctx, name)`：根 span 生成 TraceID；子 span 继承 TraceID 并记录 ParentID
- `Span.End(err)`、`SetAttr`、`Duration`、`TraceID(ctx)` 均为进程内轻量实现
- server/daemon 的日志可通过 `TaskID` 与 trace 关联（无需外部 tracing SDK）

```bash
go test ./internal/observability -run TestTrace -v
```

结果：父子 span 共享同一 TraceID，ParentID 正确；错误与属性可读取；Duration 非负。

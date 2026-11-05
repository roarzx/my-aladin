# H1 验收证据 — 结构化日志

实现：`internal/observability/logger.go`

- `NewLogger` 输出 JSON 行；`Log()` 为进程默认 logger（`ALADDIN_LOG_LEVEL`）
- `Fields{TaskID, StepID, SessionID, Attempt}.Logger(base)` 统一附加关联字段
- handler 的 `ReplaceAttr` 对敏感键与字符串值调用统一脱敏

```bash
go test ./internal/observability -run TestLogger -v
```

结果：日志可按 task_id/step_id/session_id/attempt 聚合；token 字段输出 `***`，
消息中的 Bearer 值被脱敏。

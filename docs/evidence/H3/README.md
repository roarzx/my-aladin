# H3 验收证据 — 日志与事件脱敏

实现：`pkg/protocol/redact.go`、`internal/observability/redact.go`；接入点：
logger 的 ReplaceAttr 与 `protocol.Encode`（仅 event 类型；stream 保持原文）。

规则：Bearer、`token/secret/password/authorization/api_key` 的 key=value 与 JSON 字段，
以及包含敏感词的键（如 `API_TOKEN`）递归替换为 `***`。

```bash
go test ./internal/observability -run TestRedact -v
```

结果：日志与事件编码后均不出现明文 token/密码；事件 JSON 仍保持合法结构。

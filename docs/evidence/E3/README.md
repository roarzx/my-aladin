# E3 验收证据 — 会话恢复指纹

实现：`internal/session/fingerprint.go`

- `Fingerprint(mcpCfg, env, prompt)`：SHA-256(规范化 MCP 配置 + 排序后的 env + prompt)
- `CanResume(session, fp)`：三项全等（指纹相等）且非空才允许复用
- `daemon.SessionRecord` 实现 `SessionFingerprint()`，可直接作为 `ResumableSession`

```bash
go test ./internal/session -run 'TestFingerprint|TestCanResume' -v
```

结果：env 顺序无关；MCP/env/prompt 任一变化指纹改变；空指纹与无记录均拒绝复用。

已知限制：v1 尚未在 StepHandler 中按指纹复用 ACP 会话（每步新建会话），
指纹 API 已就绪，接入点留待真实 Provider 的会话恢复策略。

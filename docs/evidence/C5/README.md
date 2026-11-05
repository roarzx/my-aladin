# C5 验收证据 — ACP 客户端与 Provider 启停

```bash
go test ./internal/transport/acp -v -count=1
```

结果：8 个用例通过（基于仓库内 fake ACP agent，不依赖真实模型）：

- 启动 → `initialize` → `session/new`；`ACPID` 正确
- `session/prompt` 的 `session/update` 被归一化为 `stream_delta` / `tool_call`
- 用量写回（inputTokens/outputTokens）
- 对 Agent 反向请求 `fs/read_text_file` 返回 **-32601**（fail-closed，v1 不启用 fs/terminal）
- `session/request_permission` 默认返回 `cancelled`
- 子进程崩溃（prompt 中途 exit）→ Prompt 返回错误
- `session/load` 复用会话（Resume）
- `refusal` stopReason 原样返回

夹具：`tests/fixtures/acp/fakeagent`（Go 小二进制，测试时自动构建），支持 `fail_init / hang / crash / refuse` 模式。

`Session.Close` 关闭 stdin 并回收子进程；daemon 接入时传入 `Reaper`，进程组由 C12 托管。

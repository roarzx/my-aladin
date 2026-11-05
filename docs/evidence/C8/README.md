# C8 验收证据 — 工具调用状态机

```bash
go test ./internal/daemon -v -count=1 -run TestTool
```

结果：3 个用例通过：

- **1 万次 tool_call / tool_result 配对**后未闭合集合为空，opened = closed = 10000
- 中断后 `Open()` 能列出未闭合调用（含 id、状态、开始时间）
- 200 个并发开始/完成，配对严格相等；未知 id 更新返回 false

状态机独立于消息历史：只维护 `id → 调用` 的映射，终态 `completed/failed/cancelled` 移出未闭合集合。

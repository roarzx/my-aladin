# A4 验收证据 — 领域类型与状态枚举

```bash
go test ./internal/domain -v -count=1 -run 'Status|AttemptAndErrorClass'
```

结果：5 个用例通过，覆盖：

- Task / Plan / Step / Attempt 的状态集合与 `Valid()`（未知状态被拒绝）
- `IsTerminal()`：Task `cancelled/failed`；Plan `stopped/completed`；Step `completed/failed/cancelled`
- Attempt 的 `unknown` 不算终态（仍需对账）
- `ErrorClass.Retryable()` 与 3.4 的重试策略一致（permission / verification 不自动重试）
- Step 不包含 `dispatched` / `running`（传输状态由 Session 承担）

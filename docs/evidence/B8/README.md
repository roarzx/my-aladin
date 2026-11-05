# B8 验收证据 — 命令下发与响应配对

```bash
go test ./internal/transport/ws -v -count=1 -run 'TestDispatch|TestRun'
```

结果：5 个用例通过（`-race` 友好，无 goroutine 泄漏）：

- **100 条并发命令**，对端**逆序**回复：每条按 `id` 配对，无一串号
- **超时**：`ctx` 到期返回 `DeadlineExceeded`，pending 表清空
- **取消**：`Cancel(id)` 立即让等待方返回 `ErrCancelled`
- **重复 id**：第二个同 id 命令被拒绝，不会覆盖已有等待者
- **连接关闭**：`Run` 返回时取消所有等待中的命令

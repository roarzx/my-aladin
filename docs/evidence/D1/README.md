# D1 验收证据 — 规划会话创建与派发

```bash
go test -tags=integration ./internal/session -v -count=1 -run TestDispatchPlanning
```

结果：4 个用例通过：

- **机器离线**：`DispatchPlanning` 返回 nil，会话置 `waiting_agent`，不下发（不报错）
- **机器在线**：会话置 `dispatched`，Dispatcher 收到正确 task/agent/machine/prompt，耗时 < **200ms**
- **派发被拒**：会话置 `dispatch_failed`，不向上返回错误
- **未接入 Dispatcher**：保持 `waiting_agent`，绝不假装已派发
- `Ensure` 对同一 (task, agent, kind) 幂等

端到端：创建任务后自动创建规划会话（真实 server + MySQL）：

```
POST /workspaces/{wid}/issues → 201
DB: sessions(kind=planning, dispatch_state=waiting_agent)
```

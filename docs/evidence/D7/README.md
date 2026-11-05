# D7 验收证据 — 任务锁

```bash
go test -tags=integration ./internal/orchestration -v -count=1 -run TestTaskLock
```

结果：2 个用例通过（真实 Redis）：

- 两个持有者竞争同一任务：第二个返回 `ErrTaskLocked`
- 释放后可被他人获取
- TTL 到期后锁自动失效，可被接管（100ms TTL 用例）

实现：`SET NX PX` 获取；每 10s 用 Lua 原子比较再续约；释放用 Lua 比较 owner 后 DEL（禁止无条件删除）。

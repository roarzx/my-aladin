# E4 验收证据 — 执行租约

实现：`internal/orchestration/lease.go`、`internal/store/redis/lease.go`

- `Acquire`（SET NX，TTL 60s）、`Renew`（Lua 比较 epoch 后续期；旧 epoch 拒绝）
- 心跳续约：daemon heartbeat 携带 `active_executions[{execution_id, lease_epoch}]`
  （9.7），Server `Hub.handleHeartbeat` 按 execution 查 attempt 并续约
- `Expire`：活跃 attempt 的租约消失 → attempt `unknown`、Step `reconciling`、
  Plan/Task `paused`（不直接判 failed），写 `step.reconciling` 审计事件

```bash
go test -tags=integration ./internal/orchestration -run TestLease -v
```

结果：当前 epoch 可续约；旧 epoch 返回 `ErrLeaseLost`；TTL 到期后 attempt→unknown、
step→reconciling。

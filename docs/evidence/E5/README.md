# E5 验收证据 — 恢复扫描（Watchdog）

实现：`internal/recovery/watchdog.go`，server 启动时按
`scheduler.recovery_scan_interval`（默认 30s）运行。

- `Sweep`：扫描执行中任务并调用 `PlanRunner.RunOnce`（无锁任务被重新接管，
  已有锁的 Runner 因抢锁失败而跳过）
- 同一轮调用 `Lease.Expire` 把过期租约转入对账
- 取消超时：`cancelling` 超过 `scheduler.cancel_timeout` 的任务落终态

```bash
go test -tags=integration ./internal/recovery -run TestWatchdogSweep -v
```

结果：执行中任务被 Runner 接管；无租约的活跃 attempt 转 unknown、Step reconciling、
Task paused；结果计数正确。

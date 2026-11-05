# F4 验收证据 — 与 PlanRunner 的任务锁互斥

实现：复用 `internal/orchestration/tasklock.go`，锁键与模式无关：
`lock:task:{task_id}`。PlanRunner 与 SquadRunner 在 server 中共享同一 `TaskLock`。

```bash
go test -tags=integration ./internal/orchestration -run 'TestRunnerMutualExclusion|TestTaskLock' -v
```

结果：另一个 Runner 持有任务锁时，SquadRunner 直接跳过，不派发、不改变工作项状态；
两个独立 TaskLock 实例（模拟两个进程）争用同一键时只有一个成功。

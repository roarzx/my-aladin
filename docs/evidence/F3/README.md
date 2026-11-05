# F3 验收证据 — SquadRunner 调度

实现：`internal/orchestration/squad_runner.go`、`composite.go`，并接入 server 的
Loop 与 Watchdog（按 `tasks.mode` 选择 Runner）。

- `RunOnce`：已有 running 工作项时直接返回；否则启动最小 ordinal 的 ready 工作项
- 交接失败把工作项置为 blocked，保留原因并记 `work_item.blocked` 审计
- `RetryHandoff`：blocked → running 后重新派发（定向重试）
- 会话复用：通过 F6 的 `ReuseOrCreate` 取负责人协作会话

```bash
go test -tags=integration ./internal/orchestration -run TestSquadRunner -v
go test -tags=integration ./internal/orchestration -run TestCompositeRunner -v
```

结果：失败交接 → blocked、定向重试成功；running 期间不再推进第二个工作项；
CompositeRunner 按任务模式选择 PlanRunner/SquadRunner。

已知限制：daemon 尚未实现 `start_squad_session`，`SquadDispatcher` 当前为空
（只推进状态）；工作项创建入口尚未暴露（F 阶段只定义模型与调度）。

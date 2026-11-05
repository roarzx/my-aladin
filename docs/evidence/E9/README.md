# E9 验收证据 — 取消流程

实现：`internal/orchestration/cancel.go`、`cancel_http.go`，路由
`POST /issues/{id}/cancel`；Watchdog 负责取消超时。

- `RequestCancel`：状态校验（executing/paused → cancelling），写
  `cancel_requested_at`；对执行中步骤向机器发 `stop_session`；未确认的执行写入
  `execution.orphaned` 审计；取消后 `RunOnce` 因任务非 executing 不再派发
- 全部执行确认停止 → 立即 `FinalizeCancel`；否则保持 cancelling，
  超过 `scheduler.cancel_timeout` 由 Watchdog 落终态
- `FinalizeCancel`：Plan→stopped、执行中/对账中 Step→cancelled、Task→cancelled
- 迟到成功回调：EventHandler 只在 Step executing 时推进，取消后的 step_done 被忽略

```bash
go test -tags=integration ./internal/orchestration -run TestCancel -v
```

结果：取消后不再派发；确认路径与超时路径都落 cancelled；迟到 step_done 不复活任务。

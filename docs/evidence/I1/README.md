# I1 验收证据 — E2E 普通模式

实现：`tests/e2e/ordinary_test.go`（真实 MySQL/Redis + 可控 Runtime）

覆盖：创建任务 → 提交/确认/启动计划 → 派发 → step_done → 失败（verification）
→ 暂停 → 人工 Retry → 完成；以及取消后迟到成功回调不复活任务。

```bash
make up && make migrate
make test-e2e
# 或：go test -tags=e2e ./tests/e2e -run TestOrdinary -v
```

结果：

- `TestOrdinary`：事件序列 `task.created → plan.submitted → step.dispatched →
  step.done → step.dispatched → step.fail → step.retried → step.done`，任务最终 `chatting`
- `TestOrdinaryCancelRecovery`：取消后任务 `cancelled`、步骤 `cancelled`；
  迟到 `step_done` 被忽略

# D6 验收证据 — 确认与启动分离

```bash
go test -tags=integration ./internal/plan -v -count=1 -run 'TestStart|TestProposeConfirmStart'
```

结果：确认与启动是两个独立动作：

- 未确认启动 → `NOT_CONFIRMED`
- 提交新 revision 后确认旧版本 → `PLAN_SUPERSEDED`
- `contentHash` 不匹配 → `HASH_MISMATCH`
- 重复确认幂等（同一 confirmation id）
- 确认后启动：任务 → `executing`，计划 → `executing`，任务版本 +1
- 再次启动 → `CONFLICT`

端到端（真实 server）：

```
POST /plans/{id}/confirm {"revision":1} → 200 {"confirmedAt":"...","revision":1}
POST /issues/{id}/start {"expectedVersion":0} → 200 {"status":"executing"}
DB: tasks.status=executing, plans.status=executing, plan_steps.status=pending
```

启动时重新校验：确认版本、任务状态与乐观锁版本（`expectedVersion` 非 0 时严格比对）。

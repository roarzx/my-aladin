# D5 验收证据 — Plan 提交与版本

```bash
go test -tags=integration ./internal/plan -v -count=1 -run TestPropose
```

结果：计划版本化提交完成：

- `revision` 递增，旧版本保留（`GetPlanByRevision` 仍可读）
- 提交的 `revision` 不是下一个 → `REVISION_CONFLICT`
- `agent_ref` 不在任务白名单 → `AGENT_REF_INVALID`
- 步骤 ordinal 唯一、title/agentRef 必填
- `contentHash` 缺省时计算 sha256
- 任务自动从 `pending` → `plan_confirming`（`plan.revised` 时保持）

端到端（真实 server）：

```
POST /issues/{id}/plans → 201 {"planId":"...","revision":1,"steps":1}
```

# B3 验收证据 — Agent Config 与可见性

```bash
go test ./internal/agent -v -count=1
go test -tags=integration ./internal/store/mysql -v -count=1 -run TestAgentVisibilityAndScheduling
```

结果：

- 单元测试 **9 个用例**：可见性枚举、`VisibleTo`（shared 对全员、private 仅创建者、非法值 fail-closed）、
  `SchedulableBy`（私有跨用户、禁用、非法可见性均返回 `AGENT_UNAVAILABLE`）、任务角色
- 集成测试通过：
  - 其他用户只看到 `shared` 且 `enabled` 的 Agent
  - 创建者看到自己的 `private` + `shared`，看不到 `disabled`
  - **shared 转 private 后，跨用户调度返回 `AGENT_UNAVAILABLE`**（B3 验收项）
  - 非创建者修改可见性返回 `sql.ErrNoRows`

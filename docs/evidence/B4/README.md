# B4 验收证据 — 任务创建与模板展开

```bash
go test ./internal/task -v -count=1
go test -tags=integration ./internal/task -v -count=1
```

结果：

- 单元测试 16 个用例：请求校验（name、1–10 个 Agent、去重、planner 必属成员、v1 单仓库、小队模式拒绝）
  与 HTTP handler（201 成功信封、非法 JSON / 未知字段 400、领域错误透传、未认证 401）
- 集成测试 4 个用例：
  - 成功创建：`tasks` / `task_agents`（a1 planner、a2 member）/ `task_repositories` 落库，
    快照包含模板引用、模板配置与 planner
  - 私有 Agent（他人所有）→ `AGENT_UNAVAILABLE`
  - 模板不存在 → `TEMPLATE_NOT_FOUND`
  - 跨工作区 Agent → `AGENT_UNAVAILABLE`

端到端（Docker MySQL/Redis + 真实 server）：

```
POST /workspaces/{wid}/issues  （无令牌）           → 401
POST /workspaces/{wid}/issues  （Bearer devtoken） → 201 {"data":{"id":"...","status":"pending"}}

DB 校验：
  task_agents: a1 planner / a2 member
  config_snapshot.template.id            = 55555555-...
  config_snapshot.templateConfig.constraints[0] = "no force push"
  config_snapshot.mode                   = ordinary
```

新增迁移 `0002_task_templates`：任务模板只在创建时展开为不可变 `config_snapshot`，后续修改模板不影响已有任务。

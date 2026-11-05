# F1 验收证据 — 小队配置快照

实现：`internal/squad/snapshot.go`、`internal/store/mysql/squad.go`、
迁移 `0003_squads`（新增 `squads` 定义表，快照仍写 `squad_snapshots`）。

- `Freeze(squadID, taskID)`：复制小队成员、负责人、协作指令、审批配置到任务快照；
  同一任务重复冻结返回已有快照（幂等）
- 创建 Issue：`squadId` 与 `agentIds/plannerAgentId` 互斥；squad 模式必须提供 `squadId`
- 快照在创建事务内写入；负责人成为任务 Planner，成员写入 `task_agents`

```bash
go test -tags=integration ./internal/squad -run TestFreeze -v
go test -tags=integration ./internal/task -run TestCreateSquadTask -v
go test ./internal/task -run TestNormalizeAndValidate -v
```

结果：冻结后修改小队定义不影响快照；重复冻结幂等；停用小队不可冻结；
建任务互斥校验与快照落库通过。

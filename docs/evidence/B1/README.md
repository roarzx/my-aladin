# B1 验收证据 — Workspace / Member 仓储

```bash
make up
go test -tags=integration ./internal/store/mysql -v -count=1 -run TestWorkspaceMembership
```

结果：`ok`，覆盖：

- 创建工作区并查询成员角色
- 重复添加成员触发主键冲突 **1062**
- 跨工作区查询返回 `sql.ErrNoRows`（由 HTTP 层映射为 403）
- 非法角色在写库前被拒绝
- 仓储同时接受 `*sql.DB` 与 `*sql.Tx`（`Executor` 接口），测试用事务自动回滚

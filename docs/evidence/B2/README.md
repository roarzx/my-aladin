# B2 验收证据 — 权限判定器

```bash
go test ./internal/auth -v -count=1
```

结果：39 个用例通过，覆盖：

- 9.5 工作区矩阵全部格子：查看 / 创建 Issue / 确认启动 / 重试 / 取消 / 审批 / 管理成员 / 管理能力
- `member` 的"仅限自己创建的"：`TaskOwnerID == actor.UserID`，owner 未知（0）时 fail-closed
- 跨工作区一律拒绝；非法角色一律拒绝
- `RequireRole` 中间件：未认证 401、角色不足 403、满足时放行
- `StaticToken`：Bearer 大小写不敏感、错误令牌/协议/缺失均拒绝、服务端未配置令牌时拒绝

`viewer 创建 Issue 返回 403` 同时由 B0 的路由测试端到端覆盖。

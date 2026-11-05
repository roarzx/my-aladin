# A2 验收证据 — 配置加载与校验

```bash
go test ./internal/config -v -count=1
```

结果：`ok`，8 个用例通过，覆盖：

- 缺 `storage.mysql_dsn` 时错误信息包含字段路径
- 未知字段（拼写错误）被拒绝（YAML `KnownFields`）
- 环境变量覆盖配置文件（`MYSQL_DSN` / `REDIS_ADDR`）
- daemon 角色不要求 storage；控制面角色要求
- 非法时长、heartbeat 窗口、默认值保留

# A6 验收证据 — MySQL 迁移

```bash
make up   # Docker: MySQL 8.0 + Redis 7
go test -tags=integration ./internal/store/mysql -v -count=1 -run TestMigrate
```

结果：`ok`，验证内容：

- DDL 从 `dev_spec.md` 9.3 抽取，共 29 张表
- 空库 up → down → up 幂等，`schema_migrations` 版本记录正确
- 插入第二个 planner 触发 **1062**（生成列 `planner_flag` + 唯一索引 `uk_one_planner`）
- 迁移脚本按分号切分时忽略字符串、反引号与注释中的分号（`sqlsplit_test.go`）

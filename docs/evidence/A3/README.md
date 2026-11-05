# A3 验收证据 — 测试基座与 CI

```bash
go test ./... -count=1
make test-integration
```

结果：

- 单测：5 个包 `ok`，不依赖外部服务
- 集成测试：基于 `deploy/docker-compose.yml` 的 MySQL 8.0 / Redis 7，全部通过
- `internal/testkit`：`NewDB`（自动回滚事务）、`NewDBRaw`、`NewRedis`，DSN 可用 `TEST_MYSQL_DSN` / `TEST_REDIS_ADDR` 覆盖
- CI：`.github/workflows/ci.yml` 包含 lint / unit / integration（服务容器）/ web 四个 job

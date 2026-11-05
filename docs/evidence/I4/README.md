# I4 验收证据 — 部署与迁移

实现：`deploy/README.md`、`deploy/docker-compose.yml`、Makefile（`up/down/reset/migrate`）。

干净机器步骤：`make up` → `make build` → `make migrate` → 启动 server →
`curl /readyz`。备份/恢复与 Redis 重建规则写在 deploy/README.md。

本机验证：

```bash
make up
export MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'
make migrate
ALADDIN_ADMIN_TOKEN=devtoken ./bin/server &
curl -sf http://127.0.0.1:8080/readyz   # {"status":"ready"}
```

结果：迁移 0001–0005 可重复执行；`/readyz` 返回 ready；MySQL/Redis 容器 healthy。

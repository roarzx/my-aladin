# A8 验收证据 — 启动自检与健康检查

```bash
make up && make build
export MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC'
export REDIS_ADDR=127.0.0.1:6379
./bin/server &
curl -s -o /dev/null -w '%{http_code}' localhost:8080/healthz
curl -s -o /dev/null -w '%{http_code}' localhost:8080/readyz
docker compose -f deploy/docker-compose.yml stop redis
curl -s -o /dev/null -w '%{http_code}' localhost:8080/readyz
docker compose -f deploy/docker-compose.yml start redis
```

结果：

| 场景 | `/healthz` | `/readyz` |
|---|---|---|
| MySQL + Redis 健康 | 200 | 200 |
| 停止 Redis | 200 | **503** |
| 恢复 Redis | 200 | 200 |

`/readyz` 除 ping 外还会校验 `schema_migrations` 版本不低于二进制期望值（未迁移时同样 503）。

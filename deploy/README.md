# 部署与迁移（I4）

## 前置

- Docker（Compose v2）
- Go 1.24+
- 可选：Node 20+ / pnpm（前端）

## 干净机器启动

```bash
git clone <repo> && cd my-aladin

# 1) 依赖（MySQL 8.0 + Redis 7）
make up                       # docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml ps   # 两个服务 healthy

# 2) 构建与迁移
make build
export MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'
export REDIS_ADDR=127.0.0.1:6379
make migrate

# 3) 启动控制面
export ALADDIN_ADMIN_TOKEN=devtoken
./bin/server &                # 默认 :8080，WS 在 /v1/acp
curl -sf http://127.0.0.1:8080/readyz   # {"status":"ready"}

# 4) 启动执行面（可选）
TOKEN=$(./bin/aladdin machine token --machine-id <machine-id> --dsn "$MYSQL_DSN" --ttl 8h | tail -1)
ALADDIN_MACHINE_TOKEN="$TOKEN" ./bin/daemon \
  --config <daemon.yaml> --profile dev --machine-id <machine-id>
```

`daemon.yaml` 最小示例见 `docs/quickstart.md`（或 E2E 证据中的配置）。

## 停止与清理

```bash
kill $(lsof -ti tcp:8080)     # 停止 server
make down                     # 停止 MySQL/Redis（保留数据卷）
make reset                    # 清空数据卷（危险）
```

## 备份与恢复

```bash
# 备份
docker exec aladdin-mysql mysqldump -uroot -proot --databases aladdin_test > backup.sql

# 恢复
docker exec -i aladdin-mysql mysql -uroot -proot < backup.sql
```

Redis 只承载可推导的热状态：整库丢失后按 9.4 的重建规则恢复（锁重新竞争、
租约过期进入对账），无需单独备份。产物与 `.meta` 目录按任务归档自行备份。

## 健康检查

| 端点 | 含义 |
|---|---|
| `GET /healthz` | 进程存活 |
| `GET /readyz` | MySQL/Redis 可用 |

验收命令：`make up && curl -sf localhost:8080/readyz`（需先启动 server）。

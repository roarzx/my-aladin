# 性能与容量基线（I3）

> 记录时间：2026-11-06；仅代表单机本地开发环境，不代表生产容量。

## 环境

| 项 | 值 |
|---|---|
| 机器 | Apple M4，10 核 |
| 系统 | darwin/arm64 |
| Go | go1.24.5 |
| MySQL | 8.0（Docker，`deploy/docker-compose.yml`） |
| Redis | 7（Docker） |

## 场景

- 30 个普通模式任务，每个任务 1 个步骤
- 并发调用 `PlanRunner.RunOnce`（各自任务锁），Runtime 为内存桩（只记录派发）
- 指标：单次 `RunOnce` 派发时延（P50/P95/P99）、总耗时、堆内存、失败数

## 复现

```bash
make up
export TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'
export TEST_REDIS_ADDR=127.0.0.1:6379
go run ./cmd/aladdin migrate --dsn "$TEST_MYSQL_DSN"
PERF_N=30 go test -tags=perf ./tests/perf -run TestBaseline -v
```

## 结果（2026-11-06）

| 指标 | 值 |
|---|---|
| N | 30 |
| 总耗时 | 70.1 ms |
| P50 | 68.8 ms |
| P95 | 70.0 ms |
| P99 | 70.0 ms |
| 失败数 | 0 |
| 堆内存 | ~1 MB |
| 实际派发 | 30 |

## 解读与限制

- 时延主要来自首次数据库连接与任务锁的 Redis 往返；30 并发下 P95 ≈ 70ms。
- 本基线不含真实 ACP Agent 执行、流式回传与事件落库，不代表端到端任务耗时。
- 未做多副本、连接池调优与长时间压测；容量结论需在目标部署环境重跑。

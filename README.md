# 阿拉丁（Aladdin）

多 Agent 研发交付平台：控制面负责计划、状态机、锁与事件；执行面以 daemon 连接
ACP Agent 执行任务；冷数据在 MySQL，可推导热状态在 Redis。

> 设计与接口契约以 [`dev_spec.md`](dev_spec.md) 为准。

## 架构概览

```
Web / CLI / API
      │
   Server（Go）── MySQL（任务/计划/尝试/事件/上下文）
      │            Redis（锁/租约/背压/未确认消息）
      │ WS v2.0（命令/响应/事件/流）
   Daemon（Go）── ACP Agent（Claude Code 等）
```

- 计划执行：`pending → plan_confirming → executing → chatting/paused/cancelled/failed`
- `completed` 表示“执行完成”，不等于“验收成功”
- 派发幂等：`execution_id` + daemon 命令去重；尝试围栏用 `attempt_no`
- 热状态全部可从 MySQL 推导；锁永不重建

## 快速启动

见 [`docs/quickstart.md`](docs/quickstart.md)。最短路径：

```bash
make up && make build && make migrate
export MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/aladdin_test?parseTime=true&loc=UTC&charset=utf8mb4'
export REDIS_ADDR=127.0.0.1:6379 ALADDIN_ADMIN_TOKEN=devtoken
./bin/server
```

## 验证

```bash
make test              # 单测（不依赖外部服务）
make verify            # Docker 依赖 + 迁移 + 集成测试 + 单测
make test-e2e          # 普通/小队端到端（真实 MySQL/Redis）
make perf-test         # 并发派发基线
pnpm --dir web test    # 前端单测（前端由独立工作流维护）
```

CI：lint / unit / integration / web 四个 job，见 `.github/workflows/ci.yml`。

## 实现状态

| 阶段 | 状态 |
|---|---|
| A 基础骨架 / B 任务与协议 / C daemon 与 ACP / D 编排与计划 / E 恢复与容错 | ✅ 完成 |
| F 小队与审批（F1–F6、F8） | ✅ 完成（F7 前端进行中） |
| G 验收流程与外部集成（G1–G7） | ✅ 完成（G8 前端进行中） |
| H 可观测与评估（H1–H6） | ✅ 完成（H7 前端进行中） |
| I E2E / 性能 / 部署 / 文档 / 证据 | ✅ 本轮完成 |
| C11 Flow 节点 Worker（执行侧） | ✅ 完成 |
| 前端完整页面 | 🚧 由另一个工作流实现 |

## 已知限制

- daemon 尚未处理 `start_planning_session` / `start_squad_session`：规划与小队的
  执行派发仍由 HTTP/CLI 驱动。
- 对账的 Git 证据（`get_git_diff` / `list_git_commits`）未接入，自动判定
  completed/failed 需人工裁决或注入证据源。
- Flow 的 environment_prepare / mars_acceptance 为 Server 侧占位执行器。
- 能力授权组件尚未接入派发前的工具清单裁剪（协议未携带工具参数）。
- 前端页面、SSO 登录与多副本部署不在本轮后端范围内。

## 文档

- [开发规格](dev_spec.md)
- [快速启动](docs/quickstart.md)
- [部署与迁移](deploy/README.md)
- [性能基线](docs/perf-baseline.md)
- [验收证据索引](docs/evidence/README.md)
- 架构决策：[ADR-0001 技术栈](docs/adr/0001-tech-stack.md)、
  [ADR-0002 存储分工](docs/adr/0002-storage-split.md)、
  [ADR-0003 协议分层](docs/adr/0003-protocol-layering.md)

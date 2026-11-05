# 验收证据索引（I6）

每项工作包的证据位于 `docs/evidence/<ID>/README.md`，包含复现命令、结果与提交 SHA。
任取一项可按 README 中的命令复现；集成/e2e/perf 用例需要 Docker 内的 MySQL/Redis。

## 复现约定

```bash
make up && make migrate
make verify                 # 单测 + 集成
make test-e2e               # 普通/小队 E2E
make perf-test              # 性能基线
```

## 证据目录

| 工作包 | 内容 |
|---|---|
| [A1](A1/README.md) | A1 验收证据 — Go module 与可运行入口 |
| [A2](A2/README.md) | A2 验收证据 — 配置加载与校验 |
| [A3](A3/README.md) | A3 验收证据 — 测试基座与 CI |
| [A4](A4/README.md) | A4 验收证据 — 领域类型与状态枚举 |
| [A5](A5/README.md) | A5 验收证据 — 状态转换守卫 |
| [A6](A6/README.md) | A6 验收证据 — MySQL 迁移 |
| [A7](A7/README.md) | A7 验收证据 — Redis 键封装与锁 |
| [A8](A8/README.md) | A8 验收证据 — 启动自检与健康检查 |
| [B0](B0/README.md) | B0 验收证据 — HTTP 基座 |
| [B1](B1/README.md) | B1 验收证据 — Workspace / Member 仓储 |
| [B2](B2/README.md) | B2 验收证据 — 权限判定器 |
| [B3](B3/README.md) | B3 验收证据 — Agent Config 与可见性 |
| [B4](B4/README.md) | B4 验收证据 — 任务创建与模板展开 |
| [B5](B5/README.md) | B5 验收证据 — FakeRuntime |
| [B6](B6/README.md) | B6 验收证据 — WS 服务端骨架 |
| [B7](B7/README.md) | B7 验收证据 — WS 消息编解码 |
| [B8](B8/README.md) | B8 验收证据 — 命令下发与响应配对 |
| [B9](B9/README.md) | B9 验收证据 — 事件去重与可靠重投 |
| [B10](B10/README.md) | B10 验收证据 — 机器注册与能力上报 |
| [C1](C1/README.md) | C1 验收证据 — Daemon 进程入口与 Profile |
| [C2](C2/README.md) | C2 验收证据 — 控制面连接（握手 / 心跳 / 重连） |
| [C3](C3/README.md) | C3 验收证据 — 命令分发与幂等 |
| [C4](C4/README.md) | C4 验收证据 — 会话管理与本地历史 |
| [C5](C5/README.md) | C5 验收证据 — ACP 客户端与 Provider 启停 |
| [C6](C6/README.md) | C6 验收证据 — 运行时探活 |
| [C7](C7/README.md) | C7 验收证据 — 流式回传与背压 |
| [C8](C8/README.md) | C8 验收证据 — 工具调用状态机 |
| [C9](C9/README.md) | C9 验收证据 — 上下文同步与版本缓存 |
| [C10](C10/README.md) | C10 验收证据 — 工作区与 Git 操作 |
| [C12](C12/README.md) | C12 验收证据 — 进程回收 |
| [C13](C13/README.md) | C13 验收证据 — FakeServer 测试夹具 |
| [D1](D1/README.md) | D1 验收证据 — 规划会话创建与派发 |
| [D2](D2/README.md) | D2 验收证据 — 规划上下文装配 |
| [D3](D3/README.md) | D3 验收证据 — 流式接收与状态机 |
| [D4](D4/README.md) | D4 验收证据 — Reply Activity 跟踪 |
| [D5](D5/README.md) | D5 验收证据 — Plan 提交与版本 |
| [D6](D6/README.md) | D6 验收证据 — 确认与启动分离 |
| [D7](D7/README.md) | D7 验收证据 — 任务锁 |
| [D8](D8/README.md) | D8 验收证据 — Step 派发 |
| [D9](D9/README.md) | D9 验收证据 — step.done 校验与计划推进 |
| [D10](D10/README.md) | D10 验收证据 — step.fail 与 Plan 暂停 |
| [D11](D11/README.md) | D11 验收证据 — 重试与 attempt 递增 |
| [D12](D12/README.md) | D12 验收证据 — 步骤摘要同步（digest 判重） |
| [D13](D13/README.md) | D13 验收证据 — 事件流与游标 |
| [D14](D14/README.md) | D14 验收证据 — 前端驾驶舱（最小） |
| [E1](E1/README.md) | E1 验收证据 — 上下文版本仲裁 |
| [E2](E2/README.md) | E2 验收证据 — 上下文注入预算 |
| [E3](E3/README.md) | E3 验收证据 — 会话恢复指纹 |
| [E4](E4/README.md) | E4 验收证据 — 执行租约 |
| [E5](E5/README.md) | E5 验收证据 — 恢复扫描（Watchdog） |
| [E6](E6/README.md) | E6 验收证据 — 对账流程 |
| [E7](E7/README.md) | E7 验收证据 — 错误分类 |
| [E8](E8/README.md) | E8 验收证据 — 背压处理 |
| [E9](E9/README.md) | E9 验收证据 — 取消流程 |
| [E10](E10/README.md) | E10 验收证据 — 故障注入套件（普通模式） |
| [F1](F1/README.md) | F1 验收证据 — 小队配置快照 |
| [F2](F2/README.md) | F2 验收证据 — WorkItem 模型与状态 |
| [F3](F3/README.md) | F3 验收证据 — SquadRunner 调度 |
| [F4](F4/README.md) | F4 验收证据 — 与 PlanRunner 的任务锁互斥 |
| [F5](F5/README.md) | F5 验收证据 — 方案版本与审批链 |
| [F6](F6/README.md) | F6 验收证据 — 小队会话与 thread 复用 |
| [F8](F8/README.md) | F8 验收证据 — 小队 CLI |
| [G1](G1/README.md) | G1 验收证据 — FlowTemplate 发布与版本 |
| [G2](G2/README.md) | G2 验收证据 — FlowRun 创建与快照 |
| [G3](G3/README.md) | G3 验收证据 — FlowRunner 固定节点 |
| [G4](G4/README.md) | G4 验收证据 — Flow 节点领取服务端 |
| [G5](G5/README.md) | G5 验收证据 — 节点重跑与下游失效 |
| [G6](G6/README.md) | G6 验收证据 — TAPD / Process 需求适配 |
| [G7](G7/README.md) | G7 验收证据 — Skill / MCP / Plugin 治理 |
| [H1](H1/README.md) | H1 验收证据 — 结构化日志 |
| [H2](H2/README.md) | H2 验收证据 — Trace 关联 |
| [H3](H3/README.md) | H3 验收证据 — 日志与事件脱敏 |
| [H4](H4/README.md) | H4 验收证据 — 指标采集 |
| [H5](H5/README.md) | H5 验收证据 — 评估 Runner |
| [H6](H6/README.md) | H6 验收证据 — 评估报告 |
| [I1](I1/README.md) | I1 验收证据 — E2E 普通模式 |
| [I2](I2/README.md) | I2 验收证据 — E2E 小队模式 |
| [I3](I3/README.md) | I3 验收证据 — 性能与容量基线 |
| [I4](I4/README.md) | I4 验收证据 — 部署与迁移 |
| [I5](I5/README.md) | I5 验收证据 — 文档收口 |
| [I6](I6/README.md) | I6 验收证据 — 证据归档 |
